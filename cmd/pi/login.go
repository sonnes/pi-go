package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/sonnes/pi-go/pkg/ai/oauth"
	"github.com/sonnes/pi-go/pkg/ai/provider/anthropic"
	"github.com/sonnes/pi-go/pkg/ai/provider/geminicli"
	"github.com/sonnes/pi-go/pkg/ai/provider/openai"
)

// oauthProvider describes how to build a login config for a provider.
type oauthProvider struct {
	name            string
	envClientID     string
	envClientSecret string
	loginConfig     func(clientID, clientSecret string) oauth.LoginConfig
}

var oauthProviders = []oauthProvider{
	{
		name:        "anthropic",
		envClientID: "ANTHROPIC_OAUTH_CLIENT_ID",
		loginConfig: func(id, _ string) oauth.LoginConfig {
			return anthropic.LoginConfig(id)
		},
	},
	{
		name:        "openai",
		envClientID: "OPENAI_OAUTH_CLIENT_ID",
		loginConfig: func(id, _ string) oauth.LoginConfig {
			return openai.LoginConfig(id)
		},
	},
	{
		name:            "google",
		envClientID:     "GOOGLE_OAUTH_CLIENT_ID",
		envClientSecret: "GOOGLE_OAUTH_CLIENT_SECRET",
		loginConfig: func(id, secret string) oauth.LoginConfig {
			return geminicli.LoginConfig(id, secret)
		},
	},
}

func loginCommand() *cli.Command {
	return &cli.Command{
		Name:      "login",
		Usage:     "Authenticate with an AI provider via OAuth",
		ArgsUsage: "[provider]",
		Action:    runLogin,
	}
}

func logoutCommand() *cli.Command {
	return &cli.Command{
		Name:      "logout",
		Usage:     "Remove stored OAuth credentials for a provider",
		ArgsUsage: "[provider]",
		Action:    runLogout,
	}
}

func runLogin(ctx context.Context, cmd *cli.Command) error {
	name := ""
	if cmd.Args().Len() > 0 {
		name = strings.ToLower(cmd.Args().First())
	}

	if name == "" {
		fmt.Fprintln(os.Stderr, "Available providers:")
		for _, p := range oauthProviders {
			fmt.Fprintf(os.Stderr, "  - %s\n", p.name)
		}
		return fmt.Errorf("usage: pi login <provider>")
	}

	prov, err := findOAuthProvider(name)
	if err != nil {
		return err
	}

	clientID := os.Getenv(prov.envClientID)
	if clientID == "" {
		return fmt.Errorf("set %s to use OAuth login for %s", prov.envClientID, prov.name)
	}

	var clientSecret string
	if prov.envClientSecret != "" {
		clientSecret = os.Getenv(prov.envClientSecret)
		if clientSecret == "" {
			return fmt.Errorf(
				"set %s to use OAuth login for %s",
				prov.envClientSecret,
				prov.name,
			)
		}
	}

	cfg := prov.loginConfig(clientID, clientSecret)
	cfg.DisplayURL = func(u string) error {
		fmt.Fprintf(os.Stderr, "\nOpen this URL in your browser to authenticate:\n\n  %s\n\n", u)
		fmt.Fprintln(os.Stderr, "Waiting for callback...")
		tryOpenBrowser(u)
		return nil
	}

	loginCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	creds, err := oauth.Login(loginCtx, cfg)
	if err != nil {
		return fmt.Errorf("login failed: %w", err)
	}

	stored, err := LoadAuth()
	if err != nil {
		return fmt.Errorf("load auth: %w", err)
	}

	stored[prov.name] = FromOAuthCredentials(creds, clientID, clientSecret)
	if err := SaveAuth(stored); err != nil {
		return fmt.Errorf("save auth: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Logged in to %s.\n", prov.name)
	return nil
}

func runLogout(ctx context.Context, cmd *cli.Command) error {
	if cmd.Args().Len() == 0 {
		return fmt.Errorf("usage: pi logout <provider>")
	}
	name := strings.ToLower(cmd.Args().First())

	stored, err := LoadAuth()
	if err != nil {
		return fmt.Errorf("load auth: %w", err)
	}

	if _, ok := stored[name]; !ok {
		return fmt.Errorf("no stored credentials for %s", name)
	}

	delete(stored, name)
	if err := SaveAuth(stored); err != nil {
		return fmt.Errorf("save auth: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Logged out of %s.\n", name)
	return nil
}

func findOAuthProvider(name string) (oauthProvider, error) {
	for _, p := range oauthProviders {
		if p.name == name {
			return p, nil
		}
	}
	return oauthProvider{}, fmt.Errorf("unknown provider: %s", name)
}

func tryOpenBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		return
	}
	_ = cmd.Start()
}
