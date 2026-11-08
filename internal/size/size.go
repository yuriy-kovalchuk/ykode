// Package size says how big something is in the words a file listing would use.
package size

import "fmt"

// Text writes a byte count as 640 B or 2.1 KB.
func Text(n int) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	return fmt.Sprintf("%.1f KB", float64(n)/1024)
}
