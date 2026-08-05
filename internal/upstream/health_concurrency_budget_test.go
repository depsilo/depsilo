//go:build !race

package upstream

import "time"

// Match GORM's production slow-SQL threshold in normal builds.
const concurrentProbePersistenceBudget = 200 * time.Millisecond
