package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/term"
)

func requireToken(app *App) (string, error) {
	if app.Config.Token == "" {
		return "", fmt.Errorf("not logged in")
	}
	return app.Config.Token, nil
}

func promptHidden(label string) (string, error) {
	fmt.Fprint(os.Stdout, label)
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stdout)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

func promptLine(label string) (string, error) {
	fmt.Fprint(os.Stdout, label)
	reader := bufio.NewReader(os.Stdin)
	text, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(text), nil
}

func parseUserIDFromToken(token string) (int, bool) {
	var claims jwt.RegisteredClaims
	_, _, err := jwt.NewParser().ParseUnverified(token, &claims)
	if err != nil {
		return 0, false
	}
	if claims.Subject == "" {
		return 0, false
	}
	id, err := strconv.Atoi(claims.Subject)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

func tokenExpiry(token string) (time.Time, bool) {
	var claims jwt.RegisteredClaims
	_, _, err := jwt.NewParser().ParseUnverified(token, &claims)
	if err != nil {
		return time.Time{}, false
	}
	if claims.ExpiresAt == nil {
		return time.Time{}, false
	}
	return claims.ExpiresAt.Time, true
}

func parseInt(value string) (int, bool) {
	v, err := strconv.Atoi(value)
	if err != nil {
		return 0, false
	}
	return v, true
}
