package hostupdate

import (
	"strconv"
	"strings"
)

type versionPart struct {
	core []int
	pre  []string
}

func CompareVersions(left, right string) int {
	a, aok := parseVersion(left)
	b, bok := parseVersion(right)
	if !aok || !bok {
		return strings.Compare(strings.TrimSpace(left), strings.TrimSpace(right))
	}
	coreLength := max(len(a.core), len(b.core))
	for index := 0; index < coreLength; index++ {
		leftPart, rightPart := 0, 0
		if index < len(a.core) {
			leftPart = a.core[index]
		}
		if index < len(b.core) {
			rightPart = b.core[index]
		}
		if leftPart < rightPart {
			return -1
		}
		if leftPart > rightPart {
			return 1
		}
	}
	if len(a.pre) == 0 && len(b.pre) > 0 {
		return 1
	}
	if len(a.pre) > 0 && len(b.pre) == 0 {
		return -1
	}
	for index := 0; index < len(a.pre) || index < len(b.pre); index++ {
		if index >= len(a.pre) {
			return -1
		}
		if index >= len(b.pre) {
			return 1
		}
		an, aerr := strconv.Atoi(a.pre[index])
		bn, berr := strconv.Atoi(b.pre[index])
		switch {
		case aerr == nil && berr == nil && an < bn:
			return -1
		case aerr == nil && berr == nil && an > bn:
			return 1
		case aerr == nil && berr != nil:
			return -1
		case aerr != nil && berr == nil:
			return 1
		case a.pre[index] < b.pre[index]:
			return -1
		case a.pre[index] > b.pre[index]:
			return 1
		}
	}
	return 0
}

func parseVersion(raw string) (versionPart, bool) {
	value := strings.TrimPrefix(strings.TrimSpace(raw), "v")
	value = strings.SplitN(value, "+", 2)[0]
	parts := strings.SplitN(value, "-", 2)
	core := strings.Split(parts[0], ".")
	if len(core) < 3 {
		return versionPart{}, false
	}
	parsed := versionPart{core: make([]int, 0, len(core))}
	for _, segment := range core {
		number, err := strconv.Atoi(segment)
		if err != nil || number < 0 {
			return versionPart{}, false
		}
		parsed.core = append(parsed.core, number)
	}
	if len(parts) == 2 && parts[1] != "" {
		parsed.pre = strings.FieldsFunc(parts[1], func(r rune) bool { return r == '.' || r == '-' })
	}
	return parsed, true
}
