// Package update checks Sidecast releases and installs verified desktop updates.
package update

import (
	"regexp"
	"strconv"
	"strings"
)

// Version is set by the release build. Development builds deliberately cannot
// replace their executable.
var Version = "dev"

var versionPattern = regexp.MustCompile(`^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-([0-9A-Za-z.-]+))?(?:\+[0-9A-Za-z.-]+)?$`)

type version struct {
	numbers [3]uint64
	pre     []string
}

func parseVersion(value string) (version, bool) {
	match := versionPattern.FindStringSubmatch(value)
	if match == nil {
		return version{}, false
	}
	var parsed version
	for index := range parsed.numbers {
		number, err := strconv.ParseUint(match[index+1], 10, 64)
		if err != nil {
			return version{}, false
		}
		parsed.numbers[index] = number
	}
	if match[4] != "" {
		parsed.pre = strings.Split(match[4], ".")
		for _, part := range parsed.pre {
			if part == "" || (len(part) > 1 && part[0] == '0' && numeric(part)) {
				return version{}, false
			}
		}
	}
	return parsed, true
}

func numeric(value string) bool {
	for _, digit := range value {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	return value != ""
}

// compareVersion returns -1, 0, or 1 according to Semantic Versioning 2.0.
func compareVersion(left, right version) int {
	for index := range left.numbers {
		if left.numbers[index] < right.numbers[index] {
			return -1
		}
		if left.numbers[index] > right.numbers[index] {
			return 1
		}
	}
	if len(left.pre) == 0 && len(right.pre) > 0 {
		return 1
	}
	if len(left.pre) > 0 && len(right.pre) == 0 {
		return -1
	}
	for index := 0; index < len(left.pre) && index < len(right.pre); index++ {
		a, b := left.pre[index], right.pre[index]
		aNumeric, bNumeric := numeric(a), numeric(b)
		switch {
		case aNumeric && bNumeric:
			if len(a) != len(b) {
				if len(a) < len(b) {
					return -1
				}
				return 1
			}
		case aNumeric:
			return -1
		case bNumeric:
			return 1
		}
		if a < b {
			return -1
		}
		if a > b {
			return 1
		}
	}
	if len(left.pre) < len(right.pre) {
		return -1
	}
	if len(left.pre) > len(right.pre) {
		return 1
	}
	return 0
}
