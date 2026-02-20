package pockettts

import (
	"fmt"
	"strconv"
)

func formatFloat(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}

func formatInt(n int) string {
	return fmt.Sprintf("%d", n)
}
