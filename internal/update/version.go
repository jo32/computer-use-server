package update

import (
	"regexp"
	"strconv"
	"strings"
)

var versionPattern = regexp.MustCompile(`^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-([0-9A-Za-z.-]+))?(?:\+([0-9A-Za-z.-]+))?$`)
var describePattern = regexp.MustCompile(`^\d+-g[0-9a-f]+`)

type version struct {
	numbers [3]uint64
	pre     string
}

func parseVersion(s string) *version {
	m := versionPattern.FindStringSubmatch(s)
	if m == nil {
		return nil
	}
	v := &version{pre: m[4]}
	for i := range 3 {
		n, err := strconv.ParseUint(m[i+1], 10, 64)
		if err != nil {
			return nil
		}
		v.numbers[i] = n
	}
	for _, field := range m[4:6] {
		if field == "" {
			continue
		}
		for _, p := range strings.Split(field, ".") {
			if p == "" {
				return nil
			}
			if field == m[4] && numeric(p) && len(p) > 1 && p[0] == '0' {
				return nil
			}
		}
	}
	return v
}

func numeric(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return s != ""
}

func Released(s string) bool {
	v := parseVersion(s)
	return v != nil && !describePattern.MatchString(v.pre) && !strings.Contains(v.pre, "dirty")
}

func Newer(a, b string) bool {
	x, y := parseVersion(a), parseVersion(b)
	if x == nil || y == nil {
		return false
	}
	for i := range 3 {
		if x.numbers[i] != y.numbers[i] {
			return x.numbers[i] > y.numbers[i]
		}
	}
	if x.pre == y.pre {
		return false
	}
	if x.pre == "" {
		return true
	}
	if y.pre == "" {
		return false
	}
	xp, yp := strings.Split(x.pre, "."), strings.Split(y.pre, ".")
	for i := 0; i < len(xp) && i < len(yp); i++ {
		a, b := xp[i], yp[i]
		if a == b {
			continue
		}
		an, bn := numeric(a), numeric(b)
		if an != bn {
			return !an
		}
		if an && len(a) != len(b) {
			return len(a) > len(b)
		}
		return a > b
	}
	return len(xp) > len(yp)
}
