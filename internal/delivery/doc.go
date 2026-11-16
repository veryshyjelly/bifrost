// Package delivery runs fenced jobs and retains frozen external-effect requests,
// immutable receipts and explicit review of uncertain outcomes.
//
// Handlers resume durable work through Effects rather than issuing untracked
// mutations. A completed effect reuses its saved receipt, while an ambiguous
// outcome stops further effects until reviewed. Store implementations must fence
// writes against expired claims even if the old handler is still running.
package delivery
