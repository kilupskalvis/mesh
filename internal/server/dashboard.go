package server

import (
	"fmt"
	"io"
	"time"

	"github.com/kalvis/mesh/internal/orchestrator"
)

// renderDashboardHTML writes a server-rendered HTML dashboard to w.
func renderDashboardHTML(w io.Writer, snap orchestrator.StateSnapshot) {
	fmt.Fprint(w, `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Mesh Dashboard</title>
<meta http-equiv="refresh" content="5">
<style>
  body { font-family: system-ui, sans-serif; margin: 2rem; background: #1a1a2e; color: #e0e0e0; }
  h1 { color: #e94560; }
  h2 { color: #0f3460; background: #16213e; padding: 0.5rem 1rem; border-radius: 4px; color: #e0e0e0; }
  table { border-collapse: collapse; width: 100%; margin-bottom: 1.5rem; }
  th, td { text-align: left; padding: 0.4rem 0.8rem; border-bottom: 1px solid #333; }
  th { background: #16213e; color: #aaa; font-size: 0.85rem; text-transform: uppercase; }
  .dim { color: #666; }
  .totals { display: flex; gap: 2rem; padding: 1rem; background: #16213e; border-radius: 4px; }
  .totals span { font-size: 1.1rem; }
  .totals .label { color: #888; font-size: 0.8rem; display: block; }
</style>
</head>
<body>
<h1>Mesh Dashboard</h1>
`)

	// Running agents
	fmt.Fprint(w, `<h2>Running Agents</h2>`)
	if len(snap.Running) == 0 {
		fmt.Fprint(w, `<p class="dim">No running agents</p>`)
	} else {
		fmt.Fprint(w, `<table>
<tr><th>Identifier</th><th>State</th><th>Last Event</th><th>Turns</th><th>Tokens</th><th>Duration</th></tr>`)
		for _, e := range snap.Running {
			dur := time.Since(e.StartedAt).Truncate(time.Second)
			fmt.Fprintf(w, `<tr><td>%s</td><td>%s</td><td>%s</td><td>%d</td><td>%d</td><td>%s</td></tr>`,
				e.Identifier, e.Issue.State, e.LastAgentEvent, e.TurnCount, e.AgentTotalTokens, dur)
		}
		fmt.Fprint(w, `</table>`)
	}

	// Retry queue
	if len(snap.RetryQueue) > 0 {
		fmt.Fprint(w, `<h2>Retry Queue</h2>
<table>
<tr><th>Identifier</th><th>Attempt</th><th>Due In</th><th>Error</th></tr>`)
		for _, e := range snap.RetryQueue {
			dueIn := time.Until(time.UnixMilli(e.DueAtMs)).Truncate(time.Second)
			if dueIn < 0 {
				dueIn = 0
			}
			errStr := ""
			if e.Error != nil {
				errStr = *e.Error
			}
			fmt.Fprintf(w, `<tr><td>%s</td><td>%d</td><td>%s</td><td>%s</td></tr>`,
				e.Identifier, e.Attempt, dueIn, errStr)
		}
		fmt.Fprint(w, `</table>`)
	}

	// Totals
	dur := time.Duration(snap.AgentTotals.SecondsRunning * float64(time.Second))
	fmt.Fprintf(w, `<h2>Totals</h2>
<div class="totals">
  <div><span class="label">Total Tokens</span><span>%d</span></div>
  <div><span class="label">Running Time</span><span>%s</span></div>
  <div><span class="label">Completed</span><span>%d</span></div>
</div>`, snap.AgentTotals.TotalTokens, dur.Truncate(time.Second), snap.Completed)

	// Rate limits
	if rl := snap.RateLimits; rl != nil {
		fmt.Fprintf(w, `
<h2>Rate Limits</h2>
<div class="totals">
  <div><span class="label">Requests</span><span>%d / %d</span></div>
  <div><span class="label">Tokens</span><span>%d / %d</span></div>
</div>`, rl.RequestsRemaining, rl.RequestsLimit, rl.TokensRemaining, rl.TokensLimit)
	}

	fmt.Fprint(w, `
</body>
</html>`)
}
