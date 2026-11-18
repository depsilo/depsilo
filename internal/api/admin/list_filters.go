package admin

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"depsilo/internal/api/contract"
)

// maxFilterEcosystems bounds a multi-value ecosystem filter. There are 15
// Ecosystems plus a few adapter aliases; anything longer is not a real filter.
const maxFilterEcosystems = 32

// ecosystemCount is the facet entry handlers that predate the contract package
// still name; new code uses contract.EcosystemCount directly.
type ecosystemCount = contract.EcosystemCount

// parseEcosystemFilter reads an ecosystem filter from the first parameter
// name that is present. Values may repeat (`?ecosystem=pypi&ecosystem=npm`)
// or be comma-separated (`?ecosystem=pypi,npm`); blanks and duplicates are
// dropped, and an empty result means no ecosystem filter.
func parseEcosystemFilter(c *gin.Context, names ...string) ([]string, error) {
	for _, name := range names {
		raw, ok := c.GetQueryArray(name)
		if !ok {
			continue
		}
		seen := make(map[string]struct{})
		values := make([]string, 0, len(raw))
		for _, entry := range raw {
			for _, part := range strings.Split(entry, ",") {
				value := strings.TrimSpace(part)
				if value == "" || value == "all" {
					continue
				}
				if _, dup := seen[value]; dup {
					continue
				}
				seen[value] = struct{}{}
				values = append(values, value)
			}
		}
		if len(values) > maxFilterEcosystems {
			return nil, fmt.Errorf("%s accepts at most %d values", name, maxFilterEcosystems)
		}
		return values, nil
	}
	return nil, nil
}

// timeWindow is an optional [Start, End] filter on a row's creation time.
type timeWindow struct {
	Start time.Time
	End   time.Time
}

// parseTimeWindow reads RFC 3339 `start` and `end` parameters. Either may be
// omitted; a malformed value or an end before the start is an error.
func parseTimeWindow(c *gin.Context) (timeWindow, error) {
	var window timeWindow
	for _, field := range []struct {
		name   string
		target *time.Time
	}{{"start", &window.Start}, {"end", &window.End}} {
		raw := strings.TrimSpace(c.Query(field.name))
		if raw == "" {
			continue
		}
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return timeWindow{}, fmt.Errorf("%s must be an RFC 3339 time", field.name)
		}
		*field.target = parsed
	}
	if !window.Start.IsZero() && !window.End.IsZero() && window.End.Before(window.Start) {
		return timeWindow{}, fmt.Errorf("end must not be before start")
	}
	return window, nil
}

// apply restricts query to the window. datetime() normalises stored zone
// suffixes so mixed legacy and UTC timestamps compare correctly.
func (w timeWindow) apply(query *gorm.DB, column string) *gorm.DB {
	if !w.Start.IsZero() {
		query = query.Where("datetime("+column+") >= datetime(?)", w.Start.UTC())
	}
	if !w.End.IsZero() {
		query = query.Where("datetime("+column+") <= datetime(?)", w.End.UTC())
	}
	return query
}

// countByEcosystem groups query by column and returns the non-empty
// ecosystems by descending count, then id: the ecosystem facet of an Admin
// list, counted under every filter except the ecosystem filter itself. The
// result is nil when no row matches.
func countByEcosystem(query *gorm.DB, column string) ([]contract.EcosystemCount, error) {
	var rows []contract.EcosystemCount
	if err := query.Select(column + " AS id, COUNT(*) AS count").Group(column).Scan(&rows).Error; err != nil {
		return nil, err
	}
	counts := rows[:0]
	for _, row := range rows {
		if row.ID != "" && row.Count > 0 {
			counts = append(counts, row)
		}
	}
	sort.Slice(counts, func(i, j int) bool {
		if counts[i].Count != counts[j].Count {
			return counts[i].Count > counts[j].Count
		}
		return counts[i].ID < counts[j].ID
	})
	return counts, nil
}
