package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"gopkg.in/yaml.v3"
)

func flagsProcess() {
	encrypt := flag.Bool("encrypt", false, "Encrypt sensitive configuration strings in the config file")

	flag.Parse()

	if *encrypt {
		encryptConfigStrings()
		marshaled, err := yaml.Marshal(config)
		if err != nil {
			log.Panic("Failed to marshal config:", err)
		}
		if err := os.WriteFile(configFile, marshaled, 0644); err != nil {
			log.Panic("Failed to write config file:", err)
		}
		fmt.Println("Configuration strings encrypted successfully.")
		os.Exit(0)
	}
}
