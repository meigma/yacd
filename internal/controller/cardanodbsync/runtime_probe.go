package cardanodbsync

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	"github.com/meigma/yacd/internal/cardano/dbsync"
	ctrlconditions "github.com/meigma/yacd/internal/ctrlkit/conditions"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	dbSyncRuntimeProbeTimeout = 3 * time.Second
	dbSyncSyncedLagThreshold  = int64(1)

	latestDBSyncBlockSQL = "select block_no, slot_no, epoch_no from block where block_no is not null order by id desc limit 1"
)

var errDBSyncSchemaPending = errors.New("db-sync schema is pending")

type cardanoDBSyncRuntimeProber interface {
	ProbePostgres(context.Context, dbSyncRuntimeProbeTarget) (dbSyncRuntimeProbeResult, error)
	Probe(context.Context, dbSyncRuntimeProbeTarget) (dbSyncRuntimeProbeResult, error)
}

type dbSyncRuntimeProbeTarget struct {
	Database       dbsync.Database
	PasswordSecret *corev1.Secret
	OgmiosURL      string
}

type dbSyncRuntimeProbeResult struct {
	Sync          *yacdv1alpha1.CardanoDBSyncProgressStatus
	PostgresReady metav1.Condition
	Synced        metav1.Condition
}

type defaultDBSyncRuntimeProber struct {
	httpClient  *http.Client
	queryDB     func(context.Context, dbSyncRuntimeProbeTarget) (dbSyncDatabaseProgress, error)
	queryOgmios func(context.Context, string) (dbSyncNodeTip, error)
}

type dbSyncDatabaseProgress struct {
	DBBlockHeight *int64
	DBSlotHeight  *int64
	Epoch         *int64
}

type dbSyncNodeTip struct {
	BlockHeight *int64
}

func (r *CardanoDBSyncReconciler) runtimeProber() cardanoDBSyncRuntimeProber {
	if r.runtimeProberOverride != nil {
		return r.runtimeProberOverride
	}

	return defaultDBSyncRuntimeProber{httpClient: http.DefaultClient}
}

func (p defaultDBSyncRuntimeProber) Probe(ctx context.Context, target dbSyncRuntimeProbeTarget) (dbSyncRuntimeProbeResult, error) {
	dbResult, err := p.ProbePostgres(ctx, target)
	if err != nil {
		return dbSyncRuntimeProbeResult{}, err
	}
	if dbResult.PostgresReady.Status != metav1.ConditionTrue {
		return dbResult, nil
	}

	sync := dbResult.Sync
	if sync == nil {
		sync = &yacdv1alpha1.CardanoDBSyncProgressStatus{}
	}

	return p.probeNodeTip(ctx, target.OgmiosURL, sync, dbResult.PostgresReady, dbResult.Synced)
}

func (p defaultDBSyncRuntimeProber) ProbePostgres(ctx context.Context, target dbSyncRuntimeProbeTarget) (dbSyncRuntimeProbeResult, error) {
	sync := &yacdv1alpha1.CardanoDBSyncProgressStatus{}

	dbCtx, cancel := context.WithTimeout(ctx, dbSyncRuntimeProbeTimeout)
	dbProgress, dbErr := p.latestDBBlock(dbCtx, target)
	cancel()

	switch {
	case dbErr == nil:
		copyDBProgress(sync, dbProgress)
	case errors.Is(dbErr, errDBSyncSchemaPending):
		return dbSyncRuntimeProbeResult{
			Sync:          nil,
			PostgresReady: ctrlconditions.Condition(conditionTypePostgresReady, metav1.ConditionTrue, conditionReasonPostgresReady, "Postgres is reachable"),
			Synced:        syncedCondition(conditionReasonPostgresSchemaPending, "db-sync has not created the block table yet"),
		}, nil
	default:
		message := fmt.Sprintf("Postgres progress query failed: %v", dbErr)
		return dbSyncRuntimeProbeResult{
			Sync:          nil,
			PostgresReady: postgresReadyCondition(conditionReasonPostgresUnavailable, message),
			Synced:        syncedCondition(conditionReasonPostgresUnavailable, "Postgres progress is unavailable"),
		}, nil
	}

	postgresReady := ctrlconditions.Condition(conditionTypePostgresReady, metav1.ConditionTrue, conditionReasonPostgresReady, "Postgres is reachable and db-sync progress query succeeded")
	if sync.DBBlockHeight == nil {
		return dbSyncRuntimeProbeResult{
			Sync:          nil,
			PostgresReady: postgresReady,
			Synced:        syncedCondition(conditionReasonSyncLagging, "db-sync has not indexed any blocks yet"),
		}, nil
	}

	return dbSyncRuntimeProbeResult{
		Sync:          sync,
		PostgresReady: postgresReady,
		Synced:        syncedCondition(conditionReasonRuntimeProbesPending, "node tip will be probed after db-sync workloads are ready"),
	}, nil
}

func (p defaultDBSyncRuntimeProber) latestDBBlock(ctx context.Context, target dbSyncRuntimeProbeTarget) (dbSyncDatabaseProgress, error) {
	if p.queryDB != nil {
		return p.queryDB(ctx, target)
	}

	password, err := postgresPassword(target.Database, target.PasswordSecret)
	if err != nil {
		return dbSyncDatabaseProgress{}, err
	}
	connString := postgresConnectionString(target.Database, password)
	conn, err := pgx.Connect(ctx, connString)
	if err != nil {
		return dbSyncDatabaseProgress{}, err
	}
	defer func() {
		_ = conn.Close(ctx)
	}()

	var blockNo, slotNo, epochNo pgtype.Int8
	if err := conn.QueryRow(ctx, latestDBSyncBlockSQL).Scan(&blockNo, &slotNo, &epochNo); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return dbSyncDatabaseProgress{}, nil
		}
		if postgresSchemaPending(err) {
			return dbSyncDatabaseProgress{}, errDBSyncSchemaPending
		}
		return dbSyncDatabaseProgress{}, err
	}

	return dbSyncDatabaseProgress{
		DBBlockHeight: pgInt8Ptr(blockNo),
		DBSlotHeight:  pgInt8Ptr(slotNo),
		Epoch:         pgInt8Ptr(epochNo),
	}, nil
}

func (p defaultDBSyncRuntimeProber) probeNodeTip(
	ctx context.Context,
	ogmiosURL string,
	sync *yacdv1alpha1.CardanoDBSyncProgressStatus,
	postgresReady metav1.Condition,
	fallbackSynced metav1.Condition,
) (dbSyncRuntimeProbeResult, error) {
	nodeCtx, cancel := context.WithTimeout(ctx, dbSyncRuntimeProbeTimeout)
	nodeTip, nodeErr := p.nodeTip(nodeCtx, ogmiosURL)
	cancel()

	if nodeErr != nil {
		if fallbackSynced.Type == "" || sync.DBBlockHeight != nil {
			fallbackSynced = syncedCondition(conditionReasonNodeTipUnavailable, fmt.Sprintf("Ogmios node tip query failed: %v", nodeErr))
		}
		return dbSyncRuntimeProbeResult{
			Sync:          sync,
			PostgresReady: postgresReady,
			Synced:        fallbackSynced,
		}, nil
	}

	sync.NodeBlockHeight = nodeTip.BlockHeight
	if sync.DBBlockHeight == nil {
		if fallbackSynced.Type == "" {
			fallbackSynced = syncedCondition(conditionReasonSyncLagging, "db-sync has not indexed any blocks yet")
		}
		return dbSyncRuntimeProbeResult{
			Sync:          sync,
			PostgresReady: postgresReady,
			Synced:        fallbackSynced,
		}, nil
	}
	if sync.NodeBlockHeight == nil {
		return dbSyncRuntimeProbeResult{
			Sync:          sync,
			PostgresReady: postgresReady,
			Synced:        syncedCondition(conditionReasonNodeTipUnavailable, "Ogmios node tip did not include a block height"),
		}, nil
	}

	lag := max(*sync.NodeBlockHeight-*sync.DBBlockHeight, 0)
	sync.LagBlocks = &lag
	if lag <= dbSyncSyncedLagThreshold {
		return dbSyncRuntimeProbeResult{
			Sync:          sync,
			PostgresReady: postgresReady,
			Synced:        ctrlconditions.Condition(conditionTypeSynced, metav1.ConditionTrue, conditionReasonSynced, "db-sync is caught up to the node tip"),
		}, nil
	}

	return dbSyncRuntimeProbeResult{
		Sync:          sync,
		PostgresReady: postgresReady,
		Synced:        syncedCondition(conditionReasonSyncLagging, fmt.Sprintf("db-sync is %d blocks behind the node tip", lag)),
	}, nil
}

func (p defaultDBSyncRuntimeProber) nodeTip(ctx context.Context, ogmiosURL string) (dbSyncNodeTip, error) {
	if p.queryOgmios != nil {
		return p.queryOgmios(ctx, ogmiosURL)
	}
	if strings.TrimSpace(ogmiosURL) == "" {
		return dbSyncNodeTip{}, errors.New("ogmios endpoint is not published")
	}

	httpURL, err := ogmiosHTTPURL(ogmiosURL)
	if err != nil {
		return dbSyncNodeTip{}, err
	}
	healthURL, err := ogmiosHealthURL(httpURL)
	if err != nil {
		return dbSyncNodeTip{}, err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, healthURL, nil)
	if err != nil {
		return dbSyncNodeTip{}, err
	}
	client := p.httpClient
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request)
	if err != nil {
		return dbSyncNodeTip{}, err
	}
	defer func() {
		_ = response.Body.Close()
	}()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return dbSyncNodeTip{}, fmt.Errorf("ogmios returned HTTP %d", response.StatusCode)
	}

	var health json.RawMessage
	if err := json.NewDecoder(response.Body).Decode(&health); err != nil {
		return dbSyncNodeTip{}, err
	}
	if len(health) == 0 {
		return dbSyncNodeTip{}, errors.New("ogmios health response is empty")
	}

	return parseOgmiosNodeTip(health)
}

func copyDBProgress(sync *yacdv1alpha1.CardanoDBSyncProgressStatus, progress dbSyncDatabaseProgress) {
	sync.DBBlockHeight = progress.DBBlockHeight
	sync.DBSlotHeight = progress.DBSlotHeight
	sync.Epoch = progress.Epoch
}

func postgresPassword(database dbsync.Database, secret *corev1.Secret) (string, error) {
	if secret == nil {
		return "", fmt.Errorf("postgres password Secret %q is missing", database.PasswordSecretName)
	}
	key := database.PasswordSecretKey
	if key == "" {
		return "", errors.New("postgres password Secret key is empty")
	}
	value, ok := secret.Data[key]
	if !ok {
		return "", fmt.Errorf("postgres password Secret key %q is missing", key)
	}
	if len(value) == 0 {
		return "", fmt.Errorf("postgres password Secret key %q is empty", key)
	}

	return string(value), nil
}

func postgresConnectionString(database dbsync.Database, password string) string {
	postgresURL := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(database.User, password),
		Host:   net.JoinHostPort(database.Host, strconv.Itoa(int(database.Port))),
		Path:   "/" + database.Name,
	}
	query := postgresURL.Query()
	if database.SSLMode != "" {
		query.Set("sslmode", database.SSLMode)
	}
	postgresURL.RawQuery = query.Encode()

	return postgresURL.String()
}

func postgresSchemaPending(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}

	switch pgErr.Code {
	case "3F000", "42P01", "42703":
		return true
	default:
		return false
	}
}

func pgInt8Ptr(value pgtype.Int8) *int64 {
	if !value.Valid {
		return nil
	}

	result := value.Int64
	return &result
}

func ogmiosEndpointURL(network *yacdv1alpha1.CardanoNetwork) string {
	if network == nil || network.Status.Endpoints == nil || network.Status.Endpoints.Ogmios == nil {
		return ""
	}

	return network.Status.Endpoints.Ogmios.URL
}

func ogmiosHTTPURL(rawURL string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	switch parsed.Scheme {
	case "ws":
		parsed.Scheme = "http"
	case "wss":
		parsed.Scheme = "https"
	case "http", "https":
	default:
		return "", fmt.Errorf("unsupported Ogmios URL scheme %q", parsed.Scheme)
	}
	if parsed.Host == "" {
		return "", errors.New("ogmios URL host is empty")
	}

	return parsed.String(), nil
}

func ogmiosHealthURL(httpURL string) (string, error) {
	parsed, err := url.Parse(httpURL)
	if err != nil {
		return "", err
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/health"
	parsed.RawQuery = ""
	parsed.Fragment = ""

	return parsed.String(), nil
}

func parseOgmiosNodeTip(data json.RawMessage) (dbSyncNodeTip, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var result any
	if err := decoder.Decode(&result); err != nil {
		return dbSyncNodeTip{}, err
	}

	height, ok := findInt64Field(result, "height", "blockHeight")
	if !ok {
		return dbSyncNodeTip{}, errors.New("ogmios tip response missing block height")
	}

	return dbSyncNodeTip{BlockHeight: &height}, nil
}

func findInt64Field(value any, keys ...string) (int64, bool) {
	switch typed := value.(type) {
	case map[string]any:
		for _, key := range keys {
			if raw, ok := typed[key]; ok {
				if value, ok := jsonInt64(raw); ok {
					return value, true
				}
			}
		}
		for _, raw := range typed {
			if value, ok := findInt64Field(raw, keys...); ok {
				return value, true
			}
		}
	case []any:
		for _, raw := range typed {
			if value, ok := findInt64Field(raw, keys...); ok {
				return value, true
			}
		}
	}

	return 0, false
}

func jsonInt64(value any) (int64, bool) {
	switch typed := value.(type) {
	case json.Number:
		result, err := typed.Int64()
		return result, err == nil
	case float64:
		result := int64(typed)
		return result, typed == float64(result)
	default:
		return 0, false
	}
}
