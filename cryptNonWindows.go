//go:build !windows

package main

import (
	"errors"
	"log"
)

type DPAPI struct {
}

func encryptConfigStrings() {
	log.Fatal("Encryption is not supported on non-Windows platforms")
}

func decryptConfigStrings() {
}

func NewDPAPI() *DPAPI {
	return nil
}

func (d *DPAPI) Encrypt(data, entropy []byte, localMachine bool) (string, error) {
	return "", errors.New("Encryption is not supported on non-Windows platforms")
}

func (d *DPAPI) Decrypt(data string, entropy []byte) ([]byte, error) {
	return nil, errors.New("Decryption is not supported on non-Windows platforms")
}
