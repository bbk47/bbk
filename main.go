package main

import (
	"fmt"
	"os"

	"github.com/bbk47/bbk/v3/cmd"
)

var (
	// 版本号
	Version = "1.0.0"
	// Git提交哈希
	GitCommitHash = "none"
)


func main() {
	cmd.SetVariables(Version,GitCommitHash)
	if err := cmd.RootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(-1)
	}
}
