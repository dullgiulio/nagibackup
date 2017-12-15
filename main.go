package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path"
	"strings"
	"sync"

	"github.com/PuerkitoBio/goquery"
)

type conf struct {
	directory     string
	url           string
	verbose       bool
	dryrun        bool
	size          int
	parallel      int
	defaultDomain string
}

// This is not thread safe as it is only read after init.
var config conf
var semaphore chan struct{}

func downloadImage(url string) {
	destination := path.Join(config.directory, path.Base(url))

	if config.verbose {
		log.Print("Saving into ", destination)
	}

	out, err := os.Create(destination)
	if err != nil {
		log.Fatal(err)
	}

	defer out.Close()

	resp, err := http.Get(url)
	if err != nil {
		log.Print("Error in HTTP GET: ", err.Error())
		return
	}
	defer resp.Body.Close()

	if config.verbose {
		log.Print("Downloading ", url)
	}

	if _, err = io.Copy(out, resp.Body); err != nil {
		log.Print("Error in image download: ", err.Error())
	}

	if err := out.Sync(); err != nil {
		log.Print("Error in image save: ", err.Error())
	}
}

func getActualImageUrl(url string) (image string) {
	doc, err := goquery.NewDocument(url)
	if err != nil {
		log.Print("Error opening document for single image: ", err.Error())
		return
	}

	doc.Find("div table").Each(func(i int, s *goquery.Selection) {
		el := s.Parent().Find("img")
		if el == nil {
			return
		}

		if val, ok := el.Attr("src"); ok {
			image = val
		}
	})

	return
}

func downloadActualImage(url string, wg *sync.WaitGroup) {
	defer wg.Done()

	imageUrl := getActualImageUrl(url)
	if imageUrl != "" {
		downloadImage(imageUrl)
	}

	if config.parallel > 0 {
		// Unblock next resource.
		<-semaphore
	}
}

func downloadImages(urls <-chan string, wg *sync.WaitGroup) {
	defer wg.Done()

	for url := range urls {
		doc, err := goquery.NewDocument(url)
		if err != nil {
			log.Print("Error opening document with all images: ", err.Error())
			continue
		}

		doc.Find("#zoom ul li a").Each(func(i int, s *goquery.Selection) {
			val, ok := s.Attr("href")
			if !ok {
				if config.verbose {
					log.Print("Href not found, skipping")
				}

				return
			}

			// TODO: Select based on setting in config.size
			if !strings.Contains(val, "size=o") {
				return
			}

			if config.parallel > 0 {
				// Blocks until one slot out of config.parallel is free.
				semaphore <- struct{}{}
			}

			wg.Add(1)
			go downloadActualImage(config.defaultDomain+val, wg)
		})
	}
}

func printImages(urls <-chan string, wg *sync.WaitGroup) {
	defer wg.Done()

	for url := range urls {
		fmt.Printf("%s\n", url)
	}
}

func extractImageUrls(urls chan<- string, doc *goquery.Document) {
	doc.Find("div.imagelog p a").Each(func(i int, s *goquery.Selection) {
		val, ok := s.Attr("href")
		if !ok {
			if config.verbose {
				log.Fatal("Href not found")
			}

			return
		}

		urls <- val
	})
}

func extractNextUrl(doc *goquery.Document) (nextUrl string) {
	doc.Find("div.pager a.navi").Each(func(i int, s *goquery.Selection) {
		val, ok := s.Attr("id")
		if !ok {
			return
		}

		if !strings.HasPrefix(val, "next_pager_") {
			return
		}

		pageNextUrl, ok := s.Attr("href")
		if !ok {
			log.Print("Invalid pager link")
		}

		nextUrl = config.defaultDomain + pageNextUrl
	})

	return
}

func fetchAllImageUrls(url string, urls chan<- string, wg *sync.WaitGroup) {
	defer wg.Done()
	defer close(urls)

	for url != "" {
		if config.verbose {
			log.Print("Fetching links from ", url)
		}

		doc, err := goquery.NewDocument(url)
		if err != nil {
			log.Fatal("Error opening document with single image: ", err)
		}

		extractImageUrls(urls, doc)

		if nextUrl := extractNextUrl(doc); url == nextUrl {
			break
		} else {
			url = nextUrl
		}
	}
}

func initDefaults() {
	config.defaultDomain = "http://nagi.ee"
	//config.size = Original
}

func parseArgs() {
	flag.IntVar(&config.parallel, "parallel", 4, "How many parallel downloads to perform; use zero to disable")
	flag.BoolVar(&config.verbose, "verbose", false, "Be verbose about progress")
	flag.BoolVar(&config.dryrun, "dry-run", false, "Only print what images would be downloaded")
	flag.Parse()

	args := flag.Args()

	if config.dryrun {
		if len(args) < 1 || args[0] == "" {
			Usage()
		}

		config.url = args[0]
		return
	}

	if len(args) < 2 {
		Usage()
	}

	if args[0] == "" || args[1] == "" {
		Usage()
	}

	config.directory = args[0]
	config.url = args[1]
}

func createDestinationDir() {
	if err := os.MkdirAll(config.directory, 0740); err != nil {
		log.Fatal(err)
	}
}

func initParallelSemaphore() {
	if config.parallel > 0 {
		semaphore = make(chan struct{}, config.parallel)
	}
}

func startDownloads() {
	var wg sync.WaitGroup

	urls := make(chan string)

	wg.Add(2)

	go fetchAllImageUrls(config.url, urls, &wg)

	if config.dryrun {
		go printImages(urls, &wg)
	} else {
		go downloadImages(urls, &wg)
	}
	wg.Wait()
}

var Usage = func() {
	fmt.Fprintf(os.Stderr, "Usage: nagibackup [<options>...] <directory> <url>\n")
	flag.PrintDefaults()
	os.Exit(1)
}

func main() {
	initDefaults()
	parseArgs()

	if !config.dryrun {
		createDestinationDir()
	}

	initParallelSemaphore()

	startDownloads()

	os.Exit(0)
}
