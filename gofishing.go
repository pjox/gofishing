package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

func walkFiles(done <-chan struct{}, root string, skip string) (<-chan string, <-chan error) {
	paths := make(chan string)
	errc := make(chan error, 1)
	go func() { // HL
		// Close the paths channel after Walk returns.
		defer close(paths) // HL
		// No select needed for this send, since errc is buffered.
		errc <- filepath.Walk(root, func(path string, info os.FileInfo, err error) error { // HL
			if err != nil {
				return err
			}
			if !info.Mode().IsRegular() {
				return nil
			}
			dir, _ := filepath.Split(path)
			if info.Mode().IsRegular() && dir == skip {
				return nil
			}
			select {
			case paths <- path: // HL
			case <-done: // HL
				return errors.New("walk canceled")
			}
			return nil
		})
	}()
	return paths, errc
}

func newFishingRequest(client *http.Client, url, path string) (*http.Request, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	query, err := os.Open("query.json")
	if err != nil {
		return nil, err
	}
	defer query.Close()

	// Prepare the reader instances to encode
	values := map[string]io.Reader{
		"file":  file,
		"query": query,
	}

	// Prepare a form to be submitted to the URL.
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for key, r := range values {
		var part io.Writer
		if x, ok := r.(io.Closer); ok {
			defer x.Close()
		}
		// Add a file
		if x, ok := r.(*os.File); ok {
			if part, err = writer.CreateFormFile(key, x.Name()); err != nil {
				return nil, err
			}
		} else {
			// Add other fields
			if part, err = writer.CreateFormField(key); err != nil {
				return nil, err
			}
		}
		if _, err := io.Copy(part, r); err != nil {
			return nil, err
		}

	}
	// Close the multipart writer.
	// so that the request won't be missing the terminating boundary.
	writer.Close()

	// Submit the form to the handler.
	request, err := http.NewRequest("POST", url, &body)
	if err != nil {
		return nil, err
	}
	// Set the content type, this will contain the boundary.
	request.Header.Set("Content-Type", writer.FormDataContentType())

	return request, err
}

func doFishingRequest(client *http.Client, path string) {
	request, err := newFishingRequest(client, "http://cloud.science-miner.com/nerd/service/disambiguate'", path)
	if err != nil {
		panic(err)
	}

	// Submit the request
	res, err := client.Do(request)
	if err != nil {
		log.Fatalln(err)
	}

	// Check the response
	if res.StatusCode != http.StatusOK {
		err = fmt.Errorf("bad status: %s", res.Status)
	}
	body := &bytes.Buffer{}
	_, err = body.ReadFrom(res.Body)
	if err != nil {
		log.Fatal(err)
	}
	res.Body.Close()

	var file []byte

	file = body.Bytes()

	var v map[string]interface{}
	if err := json.Unmarshal(file, &v); err != nil {
		panic(err)
	}

	// Format the json document
	pretty, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		panic(err)
	}
	pretty = append(pretty, '\n')

	basename := filepath.Base(path)

	var name strings.Builder
	name.WriteString("out/")
	name.WriteString(strings.TrimSuffix(basename, filepath.Ext(path)))
	name.WriteString(".json")

	out, err := os.Create(name.String())
	if err != nil {
		panic(err)
	}
	out.Write(pretty)
	out.Close()
}

func fish(root string) {
	client := &http.Client{}

	done := make(chan struct{})
	defer close(done)

	paths, errc := walkFiles(done, root, "")

	var wg sync.WaitGroup
	maxGoroutines := 10
	guard := make(chan struct{}, maxGoroutines)
	for path := range paths {
		wg.Add(1)
		go func(path string) {
			guard <- struct{}{}
			doFishingRequest(client, path)
			<-guard
			wg.Done()
		}(path)
	}

	// Check whether the Walk failed.
	if err := <-errc; err != nil { // HLerrc
		panic(err)
	}
	wg.Wait()
}

func main() {
	if len(os.Args) < 2 || len(os.Args) > 3 {
		fmt.Fprintf(os.Stderr, "usage:\n\t%s [path] [path]\n", os.Args[0])
		os.Exit(1)
	}
	fish(os.Args[1])
}
