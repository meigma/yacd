package dbsync

import "strconv"

// Environment returns the non-secret libpq environment variables a db-sync
// container needs. The Postgres password itself is loaded via PGPASSFILE so
// it never appears in the rendered environment.
func (p Plan) Environment() []EnvVar {
	return []EnvVar{
		{Name: "PGHOST", Value: p.Spec.Database.Host},
		{Name: "PGPORT", Value: strconv.Itoa(int(p.Spec.Database.Port))},
		{Name: "PGDATABASE", Value: p.Spec.Database.Name},
		{Name: "PGUSER", Value: p.Spec.Database.User},
		{Name: "PGSSLMODE", Value: p.Spec.Database.SSLMode},
		{Name: "PGPASSFILE", Value: p.Spec.Paths.PGPassFile},
	}
}
