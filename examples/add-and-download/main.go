// Command add-and-download stores a magnet and downloads its largest file.
//
//	WEBTOR_API_KEY=... go run . 'magnet:?xt=urn:btih:...'
package main

import (
	"context"
	"fmt"
	"io"
	"os"

	webtor "github.com/webtor-io/api-sdk-go"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: add-and-download <magnet|infohash>")
		os.Exit(2)
	}
	backend, err := webtor.WebUI(os.Getenv("WEBTOR_API_KEY"))
	check(err)
	c, err := webtor.New(backend)
	check(err)
	ctx := context.Background()

	res, err := c.AddResource(ctx, webtor.Magnet(os.Args[1]))
	check(err)
	fmt.Printf("stored %s (%d files, %d bytes)\n", res.Name, res.FilesCount, res.Size)

	// Single-file torrents carry the file inline; otherwise pick the largest.
	file := res.File
	if file == nil {
		for it, err := range c.ListAll(ctx, res.ID, webtor.ListOptions{Output: webtor.ListOutputFlat}) {
			check(err)
			if it.Type == webtor.ListTypeFile && (file == nil || it.Size > file.Size) {
				f := it
				file = &f
			}
		}
	}
	if file == nil {
		check(fmt.Errorf("no files in torrent"))
	}

	d, err := c.OpenDownload(ctx, res.ID, file.ID)
	check(err)
	defer d.Close()
	out, err := os.Create(d.Name)
	check(err)
	defer out.Close()
	_, err = io.Copy(out, d)
	check(err)
	fmt.Printf("downloaded %s (%d bytes)\n", d.Name, d.BytesRead())
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
