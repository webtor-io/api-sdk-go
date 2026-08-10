// Command device-login obtains an API key via the device authorization flow
// and prints it. No pre-existing credentials needed.
package main

import (
	"context"
	"fmt"
	"os"

	webtor "github.com/webtor-io/api-sdk-go"
)

func main() {
	backend, err := webtor.WebUI("") // unauthenticated: enough for the device flow
	check(err)
	c, err := webtor.New(backend)
	check(err)
	ctx := context.Background()

	host, _ := os.Hostname()
	da, err := c.StartDeviceAuth(ctx, "device-login-example @ "+host)
	check(err)

	fmt.Printf("First, copy your one-time code: %s\n", da.UserCode)
	fmt.Printf("Then open %s and confirm.\n", da.VerificationURIComplete)

	key, err := c.WaitDeviceToken(ctx, da, func() { fmt.Print(".") })
	fmt.Println()
	check(err)
	// The key is delivered exactly once — a real application persists it
	// before doing anything else.
	fmt.Printf("your API key: %s\n", key)
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
