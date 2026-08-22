//go:build !windows && !darwin

package main

import "fmt"

func main() {
	fmt.Println("sidebox は現在 Windows と macOS に対応しています。")
}
