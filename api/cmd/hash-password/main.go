package main

import (
	"bytes"
	"fmt"
	"os"

	"golang.org/x/crypto/bcrypt"
	"golang.org/x/term"
)

func main() {
	if len(os.Args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: go run ./cmd/hash-password")
		os.Exit(2)
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Fprintln(os.Stderr, "password input requires an interactive terminal")
		os.Exit(2)
	}

	password := readPassword("Пароль: ")
	confirmation := readPassword("Повторите пароль: ")
	defer clear(password)
	defer clear(confirmation)
	if len(password) == 0 {
		fmt.Fprintln(os.Stderr, "password must not be empty")
		os.Exit(1)
	}
	if !bytes.Equal(password, confirmation) {
		fmt.Fprintln(os.Stderr, "passwords do not match")
		os.Exit(1)
	}

	hash, err := bcrypt.GenerateFromPassword(password, bcrypt.DefaultCost)
	if err != nil {
		fmt.Fprintln(os.Stderr, "failed to hash password")
		os.Exit(1)
	}
	fmt.Println(string(hash))
}

func readPassword(prompt string) []byte {
	fmt.Fprint(os.Stderr, prompt)
	value, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "failed to read password")
		os.Exit(1)
	}
	return value
}

func clear(value []byte) {
	for index := range value {
		value[index] = 0
	}
}
