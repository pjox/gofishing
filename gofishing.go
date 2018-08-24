package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"rsc.io/pdf"
)

var (
	server      string
	inLocation  string
	outLocation string
	queryLoc    string
	maxnbr      int
	prettyPrint bool
)

type info struct {
	pages    int
	duration time.Duration
}

func init() {
	flag.StringVar(&server, "s", "http://cloud.science-miner.com/nerd/service/disambiguate", "the server address")
	flag.StringVar(&inLocation, "in", "in/", "the location of the PDF files")
	flag.StringVar(&outLocation, "out", "out/", "the location where the JSON files will be saved")
	flag.StringVar(&queryLoc, "q", "query.json", "the name of the query file")
	flag.IntVar(&maxnbr, "maxnb", 10, "maximun number of concurrent requests")
	flag.BoolVar(&prettyPrint, "p", false, "format the JSON documents")
}

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

	// TODO: change this for a constant,
	// unless people think they need a different query for each file.
	query, err := os.Open(queryLoc)
	if err != nil {
		return nil, err
	}

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
	request, err := newFishingRequest(client, server, path)
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

	var jsonFile []byte
	jsonFile = body.Bytes()

	// Format the json document
	if prettyPrint {
		var v map[string]interface{}
		if err := json.Unmarshal(jsonFile, &v); err != nil {
			panic(err)
		}

		jsonFile, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			panic(err)
		}
		jsonFile = append(jsonFile, '\n')
	}

	basename := filepath.Base(path)

	var name strings.Builder
	name.WriteString(outLocation)
	name.WriteString(strings.TrimSuffix(basename, filepath.Ext(path)))
	name.WriteString(".json")

	out, err := os.Create(name.String())
	if err != nil {
		panic(err)
	}
	out.Write(jsonFile)
	out.Close()
}

func fish(root string) (int, time.Duration) {
	client := &http.Client{}

	done := make(chan struct{})
	defer close(done)

	paths, errc := walkFiles(done, root, "")

	var wg sync.WaitGroup
	maxGoroutines := maxnbr
	guard := make(chan struct{}, maxGoroutines)

	infochan := make(chan info)

	for path := range paths {
		wg.Add(1)
		go func(path string) {
			guard <- struct{}{}
			start := time.Now()
			doFishingRequest(client, path)
			stop := time.Since(start)
			// Pray for this to be garbage collected
			pdf, err := pdf.Open(path)
			if err != nil {
				fmt.Println(path)
				log.Fatal(err)
			}
			infochan <- info{pdf.NumPage(), stop}
			<-guard
			wg.Done()
		}(path)
	}

	// Check whether the Walk failed.
	if err := <-errc; err != nil { // HLerrc
		panic(err)
	}
	go func() {
		wg.Wait()
		close(infochan)
	}()

	totalPages := 0
	var sysTime time.Duration

	for inform := range infochan {
		totalPages += inform.pages
		sysTime += inform.duration
	}

	return totalPages, sysTime
}

func main() {
	flag.Parse()
	start := time.Now()
	totalPages, sysTime := fish(inLocation)
	usrTime := time.Since(start)

	fmt.Printf("%d Pages where processed in:\n", totalPages)
	fmt.Printf("%v (User time)\n", usrTime)
	fmt.Printf("%v (System time)\n", sysTime)
	fmt.Println("This ammounts to:")
	fmt.Printf("%f pages/s (User time)\n", float64(totalPages)/usrTime.Seconds())
	fmt.Printf("%f pages/s (System time)\n", float64(totalPages)/sysTime.Seconds())
}
