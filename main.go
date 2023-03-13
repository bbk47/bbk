package main

import (
	"fmt"
	"gitee.com/bbk47/bbk/v3/cmd"
	"os"
)

func main() {
	if err := cmd.RootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(-1)
	}
}
