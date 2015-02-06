package main

import (
	"io"
	"log"
	"net/http"
	"os"
	"path"
	"strings"
	"sync"
	"flag"

	"github.com/PuerkitoBio/goquery"
)

type conf struct {
	directory     string
	url           string
	verbose       bool
	size		  int
	parallel	  int
	defaultDomain string
}

// This is not thread safe as it is only read after init.
var config conf
var semaphore chan struct{}

func downloadImage(url string) {
	destination := path.Join(config.directory, path.Base(url))

	if config.verbose {
		log.Printf("Saving into %s", destination)
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
		log.Printf("Downloading %s", url)
	}

	if _, err = io.Copy(out, resp.Body); err != nil {
		log.Print("Error in image download: ", err.Error())
	}
}

func getActualImageUrl(url string) (image string) {
	doc, err := goquery.NewDocument(url)
	if err != nil {
		log.Print("Error opening document for single image: ", err.Error())
		return ""
	}

	doc.Find("div table").Each(func(i int, s *goquery.Selection) {
		el := s.Parent().Find("img")
		if el != nil {
			if val, ok := el.Attr("src"); ok {
				image = val
			}
		}
	})

	return image
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
			if val, ok := s.Attr("href"); !ok {
				if config.verbose {
					log.Print("Href not found, skipping")
				}
			} else {
				// TODO: Convert to string from config.size
				if strings.Contains(val, "size=o") {
					if config.parallel > 0 {
						// Blocks until one slot out of config.parallel is free.
						semaphore <- struct{}{}
					}

					wg.Add(1)
					go downloadActualImage(config.defaultDomain + val, wg)
				}
			}
		})
	}
}

func fetchAllImageUrls(url string, urls chan<- string, wg *sync.WaitGroup) {
	defer wg.Done()

	doc, err := goquery.NewDocument(config.url)
	if err != nil {
		log.Fatal("Error opening document with single image: ", err)
	}

	doc.Find("div.imagelog p a").Each(func(i int, s *goquery.Selection) {
		if val, ok := s.Attr("href"); !ok {
			if config.verbose {
				log.Fatal("Href not found")
			}
		} else {
			urls <- val
		}
	})

	close(urls)
}

func initDefaults() {
	config.defaultDomain = "http://nagi.ee/"
	//config.size = Original
}

func parseArgs() {
	flag.IntVar(&config.parallel, "parallel", 4, "How many parallel downloads to perform; use zero to disable")
	flag.BoolVar(&config.verbose, "verbose", false, "Be verbose about progress")
	flag.Parse()

	args := flag.Args()

	if len(args) < 2 {
		log.Fatal("Usage: nagibackup <directory> <url>")
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

func main() {
	var wg sync.WaitGroup

	initDefaults()

	urls := make(chan string)

	createDestinationDir()
	initParallelSemaphore()

	wg.Add(2)

	go fetchAllImageUrls(config.url, urls, &wg)
	go downloadImages(urls, &wg)

	wg.Wait()
}
