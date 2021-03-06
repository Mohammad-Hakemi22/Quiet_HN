package main

import (
	"errors"
	"flag"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Mohammad-Hakemi22/hn/client"
)

func main() {
	// parse flags
	var port, numStories int
	flag.IntVar(&port, "port", 8000, "the port to start the web server on")
	flag.IntVar(&numStories, "num_stories", 30, "the number of top stories to display")
	flag.Parse()

	tpl := template.Must(template.ParseFiles("./index.html"))

	http.HandleFunc("/", handler(numStories, tpl))

	// Start the server
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", port), nil))
}

func handler(numStories int, tpl *template.Template) http.HandlerFunc {
	sc := stroryCache{ // created here (not inside handlerfunc) because we won't make cache for each request
		numStories: numStories,
		duration:   6 * time.Second,
	}

	go func(){
		ticker := time.NewTicker(3 * time.Second)
		for {
			temp := stroryCache{
				numStories: numStories,
				duration:   6 * time.Second,
			}
			temp.stories()
			sc.mutex.Lock()
			sc.cache = temp.cache
			sc.expiration = temp.expiration
			sc.mutex.Unlock()
			<-ticker.C
		}
	}()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		stories, err := sc.stories()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		data := templateData{
			Stories: stories,
			Time:    time.Since(start),
		}
		err = tpl.Execute(w, data)
		if err != nil {
			http.Error(w, "Failed to process the template", http.StatusInternalServerError)
			return
		}
	})
}

type stroryCache struct {
	numStories int
	cache      []item
	expiration time.Time
	duration   time.Duration
	mutex      sync.Mutex
}

func (sc *stroryCache) stories() ([]item, error) {
	sc.mutex.Lock()
	defer sc.mutex.Unlock()
	if time.Since(sc.expiration) < 0 {
		return sc.cache, nil
	}
	stories, err := getTopStories(sc.numStories)
	if err != nil {
		return nil, err
	}
	sc.expiration = time.Now().Add(sc.duration)
	sc.cache = stories
	return sc.cache, nil
}

func getTopStories(numStories int) ([]item, error) {
	var client client.Client
	var stories []item
	ids, err := client.TopItems()
	if err != nil {
		// http.Error(w, "Failed to load top stories", http.StatusInternalServerError)
		return nil, errors.New("failed to load top stories")
	}

	// having exactly 30 stories
	at := 0
	for len(stories) < numStories {
		need := (numStories - len(stories)) * (5 / 4) // for make sure we have already 30 stories id (error on get stories), getting 30 * 1.25 ids
		stories = append(stories, getStories(ids[at:at+need])...)
		at += need
	}
	return stories[:numStories], nil // return exactly 30 stories, because we have more than 30 ids (need var)
}

func getStories(ids []int) []item {
	type result struct {
		idx  int
		item item
		err  error
	}
	resultCh := make(chan result)
	for i := 0; i < len(ids); i++ {
		var client client.Client
		go func(idx, id int) {
			hnItem, err := client.GetItem(id)
			if err != nil {
				resultCh <- result{idx: idx, err: err}
			}
			resultCh <- result{idx: idx, item: parseHNItem(hnItem)}
		}(i, ids[i])
	}
	var results []result
	for i := 0; i < len(ids); i++ {
		results = append(results, <-resultCh)
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].idx < results[j].idx
	})
	var stories []item
	for _, res := range results {
		if res.err != nil {
			continue
		}
		if isStoryLink(res.item) {
			stories = append(stories, res.item)
		}
	}
	return stories
}

func isStoryLink(item item) bool {
	return item.Type == "story" && item.URL != ""
}

func parseHNItem(hnItem client.Item) item {
	ret := item{Item: hnItem}
	url, err := url.Parse(ret.URL)
	if err == nil {
		ret.Host = strings.TrimPrefix(url.Hostname(), "www.")
	}
	return ret
}

// item is the same as the hn.Item, but adds the Host field
type item struct {
	client.Item
	Host string
}

type templateData struct {
	Stories []item
	Time    time.Duration
}
