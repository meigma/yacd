export const meta = {
  name: 'session-041-review',
  description: 'Adversarial multi-dimension review of YACD session-041 CLI code (host-access verbs + YACD_* contract)',
  whenToUse: 'Reviewing the code introduced during session 041 against readability, docs, simplicity, correctness, UX, hexagonal purity, and security goals',
  phases: [
    { title: 'Review', detail: '9 dimension finders fan out over the session-041 diff' },
    { title: 'Verify', detail: '3 skeptical lenses per finding; majority vote decides' },
    { title: 'Synthesize', detail: 'completeness critic + review lead writes the final report' },
  ],
}

// --------------------------------------------------------------------------
// Shared scope brief — injected into every agent so reviewers share the exact
// review target, the architecture/quality bar, the deliberate design decisions,
// and the security model. No backticks inside (template-literal delimiter).
// --------------------------------------------------------------------------
const SCOPE = `SESSION 041 REVIEW SCOPE — YACD developer CLI, Test-Harness Phase 2.

WHAT WAS BUILT (seven squash-merged PRs on master, all under cli/ unless noted):
- #59 (02710cd): host-access kube ports + exit-code carrier. New cli/internal/kube/access.go (PrimaryPodName/Forward/Exec); extended the kube.Client port; Adapter retains the REST config + a core/v1 REST client. New cli/internal/cli/exit.go (exitError/ResolveExit). cli/cmd/yacd/main.go exit-code wiring. go.mod/go.sum.
- #60 (bd3159d): YACD_* env contract + port-forward orchestration. New cli/internal/cli/envcontract.go (hostEnv/podEnv builders); cli/internal/cli/forward.go (connectNetwork, readiness gate, requireFreshStatus shared with topup). topup.go touched. New forward_session mock.
- #61 (a94afe5): yacd run — scoped forwards + env injection + host exec + exit-code propagation + forward-drop handling. run.go, root.go wiring, doc.go.
- #62 (45c44f8): yacd exec — in-pod, argv-only (builds 'env KEY=VAL ... cmd', never a shell), socket env, TTY. exec.go. golang.org/x/term promoted to a direct dep.
- #63 (a65f379): yacd connect — supervised foreground forwards + token-free .yacd/<network>/endpoints.json at 0600, re-established on next use after a drop. connect.go; consolidated chain-endpoint vocabulary (hostBindings) in envcontract.go/forward.go; .gitignore += .yacd/.
- #66 (7a9c66c): topup --await — poll Kupo for the funded UTxO via a new UTxOConfirmer port wrapping vendored kugo. topup_await.go, topup.go, options.go, root.go. New u_tx_o_confirmer mock.
- #67 (e45ad76): docs — docs/host-access.md (the YACD_* contract reference + verb docs) and README.

FILES IN SCOPE (review the CURRENT state on master at e45ad76):
- Production (cli/internal/cli): exit.go, envcontract.go, forward.go, run.go, exec.go, connect.go, topup_await.go, and the MODIFIED topup.go, options.go, root.go, doc.go.
- Production (cli/internal/kube): access.go, and the MODIFIED client.go, doc.go.
- Entrypoint: cli/cmd/yacd/main.go.
- Tests: cli/internal/cli/{exit,envcontract,forward,run,exec,connect,topup_await}_test.go; cli/internal/kube/{access_test.go, access_envtest_test.go, client_envtest_test.go}.
- Generated (mockery output — do NOT style-review; only flag if .mockery.yml config is wrong or a port is mocked that should not be): cli/internal/mocks/{client.go, forward_session.go, u_tx_o_confirmer.go}.
- Docs: docs/host-access.md, README.md. Config: .gitignore, .mockery.yml, go.mod, go.sum.

HOW TO SEE EXACTLY WHAT CHANGED (path-scoped so it EXCLUDES the unrelated PR #64 cardano-tools work interleaved in history):
  git diff c7825f8 e45ad76 -- cli/ docs/host-access.md README.md .gitignore .mockery.yml
Base c7825f8 is PR #58 (lifecycle verbs, prior session); head e45ad76 is the tip of session 041. Inspect go.mod/go.sum directly for dependency additions. IMPORTANT: PR #64 (ad46e82, containers/cardano-tools/**) is NOT part of this review — ignore anything under containers/.

Focus on code ADDED or CHANGED in session 041. Flag a pre-existing issue only if a session-041 change makes it newly relevant or worsens it, and label it "pre-existing".

ARCHITECTURE & CONVENTIONS (the bar this code must meet):
- Hexagonal port/adapter: kube.Client is the PORT consumed by the cli command layer; Adapter is the controller-runtime + REST implementation; kube.NewClient returns the concrete *Adapter (Rule 7 — lifecycle owners hold a typed value) while the command layer holds the Client interface for testability. The host-access seam extends the SAME Client port with PrimaryPodName/Forward/Exec. The CLI stays api/v1alpha1-pure: NO imports of the operator internal/... packages. PrimaryPodName must resolve the primary Pod from the operator PUBLISHED node-to-node Service selector, not controller-internal labels. The node container name and /ipc/node.socket are intentionally pinned as CLI-local constants (the controller const is unexported, so a guard test is impossible). New ports this session: UTxOConfirmer (topup --await, wraps kugo) and a forwardSession abstraction — check they sit on the correct side of the boundary, are minimal, and are mockable.
- Each package has a doc.go contract describing its surface; exported symbols carry godoc; packages keep per-command / per-responsibility file decomposition. Typed vocabulary (e.g. kube.ConditionType) over stringly-typed values. Mockery v3 + Testify is the test stack; mocks live in cli/internal/mocks, regenerated via 'moon run root:generate' driven by .mockery.yml.
- go-style: modular packages, restrained inline comments (comment the WHY, not the WHAT), disciplined godoc, hexagonal seams. Match the idiom of the surrounding cli/ and kube/ code.

DELIBERATE DESIGN DECISIONS this session made (do NOT naively re-flag these as bugs; challenge one only with strong, concrete evidence it is actually wrong):
- exec is argv-only: it builds 'env KEY=VAL ... cmd' and never invokes a shell, so $VAR is NOT expanded in-pod. Intentional (shell-injection avoidance + socket tooling).
- exec OMITS YACD_FAUCET_TOKEN in-pod: a Bearer token placed in PodExecOptions.Command would leak to apiserver audit logs and /proc. The faucet token is HOST-ONLY — never set in-pod, never written to endpoints.json.
- Host URLs DERIVE their scheme from the operator published status URL (Ogmios stays ws://), not hard-coded.
- topup --await REQUIRES Kupo via --kupo-url / YACD_KUPO_URL and does NOT self-forward (standalone topup also needs a reachable faucet; under yacd run both URLs are already injected).
- connect detects a dropped forward LAZILY (on next use), documented in runConnect — acceptable for an idle session.
- topup_trust.go (pre-existing, security-load-bearing): validateFaucetURLTrust gates sending the Secret token to any non-loopback faucet URL; tests assert GetSecretValue is NOT called when the gate fails. The new --await / kupo-url paths must not weaken this.

SECURITY MODEL (test-environment safety is the TOP priority):
- The faucet auth token must never leak: host-only (not in-pod, not in audit logs, not in endpoints.json, not in error/log output) and only sent to loopback / explicitly-trusted destinations.
- endpoints.json is written token-free at 0600 under .yacd/<network>/ (gitignored).
- exec is shell-free / argv-only.
- New non-loopback URL inputs (kupo-url) deserve scrutiny for SSRF / exposure / eavesdropping and may warrant the same trust posture as faucet-url where a secret could be exposed.`

// --------------------------------------------------------------------------
// Review dimensions — one finder each. Maps onto the user's seven goals
// (Correctness split into command-layer + adapter-mechanics; Tests added as a
// cross-cutting quality lens).
// --------------------------------------------------------------------------
const DIMENSIONS = [
  {
    key: 'readability',
    title: 'Readability & maintainability',
    focus: 'Naming clarity and consistency; file/function decomposition and cohesion; function length and nesting depth; whether the code reads like the surrounding cli/ and kube/ code (idiom, structure); dead or duplicated code; magic values that should be named constants; whether a future maintainer can follow the control flow — especially the port-forward goroutines in forward.go/access.go and the run/exec/connect orchestration.',
  },
  {
    key: 'docs',
    title: 'Documentation',
    focus: 'Are the package doc.go contracts (cli and kube) accurate and complete after the session-041 additions? Do all newly-exported symbols carry correct, useful godoc? Are WHY-comments present where the code is non-obvious (the security rationale for argv-only exec, no-faucet-token-in-pod, lazy forward-drop)? Is docs/host-access.md correct and consistent with the actual flags/behavior (verbs, the YACD_* table, examples that actually work)? Is the README accurate? Flag stale/missing/contradictory docs and any --help/usage text that misleads.',
  },
  {
    key: 'simplicity',
    title: 'Simplicity & straightforwardness',
    focus: 'Over-engineering, unnecessary abstraction or indirection, premature generality, code more complex than the problem requires and thus more error-prone. Identify places with a materially simpler equivalent (fewer moving parts, fewer goroutines, clearer data flow) WITHOUT losing correctness or the hexagonal boundary. Flag confusing control flow, redundant state, and accidental complexity.',
  },
  {
    key: 'correctness-cli',
    title: 'Correctness — command layer',
    focus: 'run.go, exec.go, connect.go, topup_await.go, topup.go, envcontract.go, options.go, root.go, exit.go, cmd/yacd/main.go. Flag: flag/argument parsing and precedence (flags vs env vs config); exit-code propagation and ResolveExit logic; error handling and wrapping; context cancellation / signal (SIGINT/SIGTERM) handling; edge cases (missing args, empty cmd dropping to $SHELL, unset env, not-ready network, NotFound). Verify behavior matches what doc.go and docs/host-access.md promise.',
  },
  {
    key: 'correctness-kube',
    title: 'Correctness — adapter & host-access mechanics',
    focus: 'access.go and client.go. PrimaryPodName Service-selector resolution and not-found semantics; Forward port-forward setup, readiness-channel handling, goroutine lifecycle and shutdown, error propagation, resource/connection leaks; Exec stream wiring (stdin/stdout/stderr/TTY), SPDY/exec executor usage, exit-status surfacing; REST config/client correctness; ErrNotFound wrapping consistency with the rest of the port. Hunt for races, leaked goroutines, unclosed resources, and swallowed errors.',
  },
  {
    key: 'ux',
    title: 'End-user UX & feedback',
    focus: 'Are error messages actionable and free of leaking internals/secrets? Are failures (network not ready, pod not found, forward drop, missing --kupo-url, mainnet, untrusted faucet URL) surprising or well-signposted? Is there adequate feedback during long/blocking operations (connect holding forwards, topup --await polling)? Is --help/usage clear and discoverable? Does exit-code behavior match user expectation for scripting? Reducing surprises and giving strong feedback is a primary goal — flag anything that would confuse or silently mislead a developer.',
  },
  {
    key: 'hexagonal',
    title: 'Architectural purity (hexagonal)',
    focus: 'Does the command layer depend ONLY on the kube.Client port (never type-asserting to *Adapter, never importing controller internal/... packages, staying api/v1alpha1-pure)? Are the new ports (Forward/Exec/PrimaryPodName on Client, the UTxOConfirmer port, the forwardSession abstraction) on the correct side of the boundary, minimal, and mockable? Is NewClient Rule-7 concrete return preserved? Is domain/vocabulary placed correctly (kube vs cli)? Are the doc.go contracts honored by the actual code? Flag any boundary leak, hidden coupling, or abstraction in the wrong package.',
  },
  {
    key: 'security',
    title: 'Security & test-environment safety',
    focus: 'TOP priority. Trace the faucet auth token end-to-end and prove it cannot leak — confirm it is never injected in-pod (exec), never written to endpoints.json, never printed in logs/errors, and only sent to loopback/trusted destinations (verify topup_trust.go gate still holds for the new --await/kupo-url paths). Verify exec is genuinely shell-free/argv-only (no $VAR expansion, no injection vector). Verify endpoints.json is 0600, token-free, and under a gitignored path. Check the new --kupo-url / non-loopback inputs for SSRF/exposure/eavesdropping and whether they deserve the same trust posture as faucet-url. Check scheme derivation does not silently downgrade ws/wss or http/https. Flag any token in PodExecOptions.Command, any world-readable secret file, any plaintext-over-network secret exposure, and any path that sends a secret somewhere not status-published or explicitly trusted.',
  },
  {
    key: 'tests',
    title: 'Test quality',
    focus: 'The cli + kube _test.go files added this session. Are tests behavior-first and table-driven where appropriate, using Testify (assert/require) and mockery mocks per the repo stack? Do they cover the real risk surface (exit-code propagation, env-contract host-vs-pod variable sets incl. faucet-token-host-only, forward readiness/drop, exec argv-only & token omission, connect endpoints.json contents/perms, topup --await polling/timeout, PrimaryPodName resolution)? Is the envtest-vs-unit split correct (access_envtest_test.go for live-cluster paths)? Flag over-mocking, assertion-free tests, missing critical-path coverage, brittle tests. NOTE: Forward/Exec need a live kubelet and are intentionally NOT envtested — do not flag that absence.',
  },
]

// Three diverse verification lenses applied to every finding.
const LENSES = [
  { key: 'accuracy', instruction: 'CLAIM ACCURACY: Open the cited file and read the exact lines. Does the code actually exist and behave the way the finding describes? If the finding misreads, misquotes, or mislocates the code, REFUTE.' },
  { key: 'impact', instruction: 'REAL-WORLD IMPACT: Granting the code is roughly as described, does this actually matter — a real bug, security exposure, UX failure, or maintenance hazard that would manifest in practice — or is it purely theoretical/cosmetic? Downgrade trivia; REFUTE non-issues.' },
  { key: 'intent', instruction: 'INTENTIONAL-OR-HANDLED: Is this an intentional, documented design decision (see the DECISIONS list in the scope) or already handled/mitigated elsewhere (another file, a guardrail, a test, a defaulting path)? Search the code to confirm. If decided or already handled, REFUTE or downgrade and say where.' },
]

const FINDINGS_SCHEMA = {
  type: 'object',
  additionalProperties: false,
  required: ['dimension', 'summary', 'findings'],
  properties: {
    dimension: { type: 'string' },
    summary: { type: 'string', description: 'One to two sentence overall read of the session-041 code on this dimension.' },
    findings: {
      type: 'array',
      items: {
        type: 'object',
        additionalProperties: false,
        required: ['id', 'title', 'severity', 'file', 'lines', 'description', 'evidence', 'recommendation', 'confidence'],
        properties: {
          id: { type: 'string', description: 'Form <dimensionKey>-N, N starting at 1.' },
          title: { type: 'string' },
          severity: { type: 'string', enum: ['critical', 'high', 'medium', 'low', 'nit'] },
          file: { type: 'string', description: 'Repo-relative path.' },
          lines: { type: 'string', description: 'Line or range, e.g. 42-50.' },
          description: { type: 'string', description: 'What it is and why it matters.' },
          evidence: { type: 'string', description: 'Specific code reference / snippet supporting the finding.' },
          recommendation: { type: 'string' },
          confidence: { type: 'string', enum: ['high', 'medium', 'low'] },
        },
      },
    },
  },
}

const VERDICT_SCHEMA = {
  type: 'object',
  additionalProperties: false,
  required: ['findingId', 'lens', 'verdict', 'reasoning', 'adjustedSeverity'],
  properties: {
    findingId: { type: 'string' },
    lens: { type: 'string' },
    verdict: { type: 'string', enum: ['confirmed', 'refuted', 'uncertain'] },
    reasoning: { type: 'string' },
    adjustedSeverity: { type: 'string', enum: ['critical', 'high', 'medium', 'low', 'nit', 'none'] },
  },
}

const GAPS_SCHEMA = {
  type: 'object',
  additionalProperties: false,
  required: ['gaps', 'underCoveredFiles'],
  properties: {
    gaps: {
      type: 'array',
      items: {
        type: 'object',
        additionalProperties: false,
        required: ['area', 'concern', 'suggestedCheck'],
        properties: {
          area: { type: 'string' },
          concern: { type: 'string' },
          suggestedCheck: { type: 'string' },
        },
      },
    },
    underCoveredFiles: { type: 'array', items: { type: 'string' } },
  },
}

const finderPrompt = (d) =>
  'You are a senior Go engineer performing a focused code review of YACD session-041 CLI code. Your review dimension is: ' + d.title + '.\n\n' +
  SCOPE + '\n\n' +
  'YOUR DIMENSION — ' + d.title + ':\n' + d.focus + '\n\n' +
  'HOW TO REVIEW:\n' +
  '1. Read the in-scope files listed above in full (they are small). Use the path-scoped git diff to see what is new vs modified.\n' +
  '2. Ground EVERY finding in a concrete file and line range from the CURRENT tree (file:line must be accurate and clickable).\n' +
  '3. Report only genuine issues within your dimension. Do NOT manufacture findings; an empty list is a valid, good result. Nits are allowed but keep them few and mark them severity nit.\n' +
  '4. Do not naively re-flag the intentional DECISIONS listed in the scope; challenge one only with strong, concrete evidence.\n' +
  '5. Severity: critical = security hole / data-loss / breaks a core path; high = real bug or strong harm to your dimension; medium = clear issue worth fixing; low = minor; nit = style/preference.\n' +
  '6. Generated mocks under cli/internal/mocks are mockery output — do not style-review them.\n\n' +
  'Assign each finding an id of the form ' + d.key + '-N (N starting at 1). Provide a one-to-two-sentence overall summary of how the session-041 code fares on your dimension. Return strictly via the structured-output tool.'

const verifyPrompt = (f, lens) =>
  'You are an independent, skeptical code reviewer verifying ONE finding from a review of YACD session-041 CLI code. Your job is to try to BREAK the finding, not to agree with it.\n\n' +
  SCOPE + '\n\n' +
  'THE FINDING UNDER REVIEW (id ' + f.id + ', dimension ' + f.dimension + '):\n' +
  '- Title: ' + f.title + '\n' +
  '- Severity claimed: ' + f.severity + ' (reporter confidence ' + f.confidence + ')\n' +
  '- Location: ' + f.file + ' ' + (f.lines || '') + '\n' +
  '- Description: ' + f.description + '\n' +
  '- Evidence cited: ' + (f.evidence || '(none given)') + '\n' +
  '- Recommendation: ' + f.recommendation + '\n\n' +
  'YOUR VERIFICATION LENS — ' + lens.instruction + '\n\n' +
  'Independently inspect the actual current code (read ' + f.file + ' and anything related). Apply ONLY your lens above. Then return a verdict: confirmed (real and matters under your lens), refuted (wrong, does not matter, or an intentional/handled decision), or uncertain (cannot determine from the code alone). Be decisive — refute intentional documented decisions unless you have concrete new evidence they are actually harmful. Set adjustedSeverity to what YOU think it should be (none if refuted). Echo findingId=' + f.id + ' and lens=' + lens.key + '. Return strictly via the structured-output tool.'

const criticPrompt = (confirmed, dimSummaries) =>
  'You are a completeness critic for a multi-dimension review of YACD session-041 CLI code. The review covered: readability, documentation, simplicity, correctness (command layer + adapter mechanics), UX, hexagonal architecture, security, and tests.\n\n' +
  SCOPE + '\n\n' +
  'The review produced these CONFIRMED findings (title + location):\n' +
  (confirmed.map((c) => '- [' + c.dimension + '/' + c.severity + '] ' + c.title + ' (' + c.file + ' ' + (c.lines || '') + ')').join('\n') || '(none)') + '\n\n' +
  'Per-dimension reviewer summaries:\n' +
  (dimSummaries.map((s) => '- ' + s.dimension + ': ' + s.summary).join('\n')) + '\n\n' +
  'Identify what the review may have MISSED: in-scope files that received little or no scrutiny; cross-file or integration issues a single-dimension reviewer would not see; a user goal that is under-covered; or a contract/claim asserted but not actually verified against the code. For each gap give the area, the concern, and a concrete suggested check. Also list any in-scope files you believe are under-covered. Return strictly via the structured-output tool.'

const synthPrompt = (payload) =>
  'You are the review lead writing the FINAL report for a multi-agent review of the code introduced in YACD session 041 (Test-Harness Phase 2: the run/exec/connect host-access verbs, topup --await, the YACD_* contract, and host-access docs).\n\n' +
  SCOPE + '\n\n' +
  'You are given the VERIFIED findings (each independently checked by three skeptical verifiers under different lenses), the per-dimension summaries, the completeness-critic gaps, and the count of refuted findings. Do NOT invent findings beyond this input. DATA (JSON):\n' +
  JSON.stringify(payload) + '\n\n' +
  'Write a precise, skimmable Markdown report with these sections:\n\n' +
  '1. Executive summary — overall assessment of the session-041 code against the user seven goals (readability/maintainability, documentation, simplicity, correctness, UX, hexagonal purity, security/test-env safety). State the headline counts (confirmed findings by severity) and the single most important takeaway. Be honest: if the code is largely solid, say so.\n' +
  '2. Findings — grouped under the seven goal areas (map correctness-cli + correctness-kube under Correctness; include a Test-quality subsection). Within each group order by severity (critical to nit). For each finding render:\n' +
  '   **[SEVERITY] Title** — file:line\n' +
  '   - What & why it matters (1-3 sentences).\n' +
  '   - Recommendation (concrete).\n' +
  '   - Verification: verifier consensus (e.g. 3/3 confirmed) and your confidence; if verifiers adjusted severity, use the adjusted value and say so.\n' +
  '   Omit a goal area with no findings, noting it clean in one line.\n' +
  '3. Worth a human glance — the uncertain findings (verifiers split), one line each.\n' +
  '4. Coverage gaps — from the completeness critic; what was not deeply verified and is worth a manual look.\n' +
  '5. Considered and dismissed — one line: how many findings were refuted, plus any notable false-positive worth recording (so the reader trusts the filter).\n' +
  '6. Recommended actions — a prioritized numbered short list (top 3-7) of what to fix first, framed for a maintainer.\n\n' +
  'Use accurate file:line references throughout. No hype, no filler. This report is the deliverable; return it as Markdown text (no structured-output tool).'

// --------------------------------------------------------------------------
// Orchestration
// --------------------------------------------------------------------------
log('Reviewing session-041 CLI code across ' + DIMENSIONS.length + ' dimensions; each finding gets 3 adversarial verifiers.')

phase('Review')
const results = await pipeline(
  DIMENSIONS,
  (d) => agent(finderPrompt(d), { label: 'review:' + d.key, phase: 'Review', schema: FINDINGS_SCHEMA }),
  async (review, d) => {
    if (!review || !review.findings || review.findings.length === 0) {
      return { dimension: d.key, summary: (review && review.summary) || '(no findings)', items: [] }
    }
    const items = await parallel(
      review.findings.map((f) => async () => {
        const finding = { ...f, dimension: d.key }
        const verdicts = (
          await parallel(
            LENSES.map((lens) => () =>
              agent(verifyPrompt(finding, lens), { label: 'verify:' + f.id + ':' + lens.key, phase: 'Verify', schema: VERDICT_SCHEMA }),
            ),
          )
        ).filter(Boolean)
        return { finding, verdicts }
      }),
    )
    return { dimension: d.key, summary: review.summary, items: items.filter(Boolean) }
  },
)

const dims = results.filter(Boolean)
const dimSummaries = dims.map((d) => ({ dimension: d.dimension, summary: d.summary }))
const allItems = dims.flatMap((d) => d.items)

// Classify by majority vote across the three lenses.
const classified = allItems.map(({ finding, verdicts }) => {
  const confirmedVotes = verdicts.filter((v) => v.verdict === 'confirmed').length
  const refutedVotes = verdicts.filter((v) => v.verdict === 'refuted').length
  let status = 'uncertain'
  if (confirmedVotes >= 2) status = 'confirmed'
  else if (refutedVotes >= 2) status = 'refuted'
  // Adjusted severity: most severe non-none severity among confirming verdicts, else the reporter severity.
  const order = { critical: 5, high: 4, medium: 3, low: 2, nit: 1, none: 0 }
  const adj = verdicts
    .filter((v) => v.verdict === 'confirmed' && v.adjustedSeverity && v.adjustedSeverity !== 'none')
    .map((v) => v.adjustedSeverity)
  const effectiveSeverity = adj.length
    ? adj.reduce((a, b) => (order[b] > order[a] ? b : a))
    : finding.severity
  return { ...finding, verdicts, confirmedVotes, refutedVotes, status, effectiveSeverity }
})

const confirmed = classified.filter((c) => c.status === 'confirmed')
const uncertain = classified.filter((c) => c.status === 'uncertain')
const refuted = classified.filter((c) => c.status === 'refuted')

log('Verification done: ' + confirmed.length + ' confirmed, ' + uncertain.length + ' uncertain, ' + refuted.length + ' refuted across ' + allItems.length + ' raw findings.')

phase('Synthesize')
const critic = await agent(criticPrompt(confirmed, dimSummaries), { label: 'completeness-critic', phase: 'Synthesize', schema: GAPS_SCHEMA })

const payload = {
  goals: [
    'readability/maintainability', 'documentation', 'simplicity', 'correctness',
    'UX/feedback', 'hexagonal purity', 'security/test-env safety',
  ],
  dimensionSummaries: dimSummaries,
  confirmed,
  uncertain,
  refutedCount: refuted.length,
  refutedSamples: refuted.map((r) => ({ title: r.title, file: r.file, lines: r.lines, dimension: r.dimension })),
  coverageGaps: (critic && critic.gaps) || [],
  underCoveredFiles: (critic && critic.underCoveredFiles) || [],
}

const report = await agent(synthPrompt(payload), { label: 'synthesis', phase: 'Synthesize' })

return {
  report,
  counts: { confirmed: confirmed.length, uncertain: uncertain.length, refuted: refuted.length, raw: allItems.length },
}
