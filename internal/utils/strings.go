package utils

import "fmt"

func Strings[T fmt.Stringer](vs ...T) []string {
	strs := make([]string, 0, len(vs))
	for i, v := range vs {
		strs[i] = v.String()
	}
	return strs
}
