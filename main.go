package main

import (
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
	size		  int
	defaultDomain string
}

// This is not thread safe as it is only read after init.
var config conf

func downloadImage(url string) {
	out, err := os.Create(path.Join(config.directory, path.Base(url)))
	defer out.Close()

	resp, err := http.Get(url)
	if err != nil {
		log.Printf(err.Error())
		return
	}
	defer resp.Body.Close()

	if config.verbose {
		log.Printf("Downloading %s", url)
	}

	if _, err = io.Copy(out, resp.Body); err != nil {
		log.Print(err.Error())
	}
}

func getActualImageUrl(url string) (image string) {
	doc, err := goquery.NewDocument(url)
	if err != nil {
		log.Print(err.Error())
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
}

func downloadImages(urls <-chan string, wg *sync.WaitGroup) {
	defer wg.Done()

	for url := range urls {
		doc, err := goquery.NewDocument(url)
		if err != nil {
			log.Print(err.Error())
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
					// TODO: Just cap this to some max sync jobs.
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
		log.Fatal(err)
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

func main() {
	var wg sync.WaitGroup

	// TODO: Do args parsing
	config.directory = os.Args[1]
	config.url = os.Args[2]
	config.verbose = true
	config.defaultDomain = "http://nagi.ee/"
	// config.size = Original

	urls := make(chan string)

	wg.Add(2)

	go fetchAllImageUrls(config.url, urls, &wg)
	go downloadImages(urls, &wg)

	wg.Wait()
}
