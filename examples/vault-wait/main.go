// Command vault-wait pledges a resource to the Vault and waits until the
// transfer completes.
//
//	WEBTOR_API_KEY=... go run . 08ada5a7a6183aae1e09d831df6748d566095a10
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	webtor "github.com/webtor-io/api-sdk-go"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: vault-wait <resource-id>")
		os.Exit(2)
	}
	rid := os.Args[1]
	backend, err := webtor.WebUI(os.Getenv("WEBTOR_API_KEY"))
	check(err)
	c, err := webtor.New(backend)
	check(err)
	ctx := context.Background()

	if _, err := c.VaultPledge(ctx, rid); err != nil && !webtor.IsConflict(err) {
		check(err) // conflict = already pledged, keep waiting
	}
	for {
		st, err := c.VaultPledgeStatus(ctx, rid)
		check(err)
		switch st.Status {
		case webtor.PledgeStatusVaulted:
			fmt.Println("vaulted")
			return
		case webtor.PledgeStatusExpired:
			check(fmt.Errorf("pledge expired"))
		case webtor.PledgeStatusFailed:
			// Terminal for the attempt, not the resource: storage retries on
			// its own schedule, so keep polling instead of re-pledging.
			fmt.Println("transfer attempt failed, storage will retry — still waiting")
		default:
			if st.Progress != nil {
				fmt.Printf("%s %.1f%%\n", st.Status, *st.Progress)
			} else {
				fmt.Println(st.Status)
			}
		}
		time.Sleep(15 * time.Second)
	}
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
