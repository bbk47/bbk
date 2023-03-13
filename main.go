package main

import (
	"fmt"
	"gitee.com/bbk47/bbk/cmd"
	"os"
)

func main() {
	if err := cmd.RootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(-1)
	}
}
