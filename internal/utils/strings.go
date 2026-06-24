package utils

import "fmt"

func Strings[T fmt.Stringer](vs ...T) []string {
	strs := make([]string, 0, len(vs))
	for _, v := range vs {
		strs = append(strs, v.String())
	}
	return strs
}
