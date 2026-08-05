//go:build race

package upstream

import "time"

// Race instrumentation slows every database/sql boundary substantially. Keep
// the same concurrent behavior and integrity assertions with a wider budget.
const concurrentProbePersistenceBudget = 500 * time.Millisecond
