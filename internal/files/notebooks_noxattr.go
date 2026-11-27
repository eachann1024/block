//go:build unix && !darwin

package files

func readNotebookXattr(string) string    { return "" }
func writeNotebookXattr(string, string) {}
func clearNotebookXattr(string)         {}
