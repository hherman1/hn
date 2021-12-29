package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/carlmjohnson/requests"

	md "github.com/JohannesKaufmann/html-to-markdown"
)

// base url for the API
const base = "https://hacker-news.firebaseio.com"

func main() {
	if err := run(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

type item struct {
	Id int `json:"id"`
	Title string `json:"title"`
	Score int `json:"score"`
	By string `json:"by"`
	Url string `json:"url"`
	Descendants int `json:"descendants"`
	Type string `json:"type"`
	Kids []int `json:"kids"`
	Text string `json:"text"`
}

func run() error {
	ctx := context.Background()
	if len(os.Args) == 1 {
		err := top(ctx)
		if err != nil {
			return fmt.Errorf("load top: %w", err)
		}
		return nil
	}
	if os.Args[1] == "comments" {
		args := os.Args[2:]
		if len(args) != 1 {
			return fmt.Errorf("comments: expected exactly 1 arg, got %v", len(args))
		}
		id, err := strconv.Atoi(args[0])
		if err != nil {
			return fmt.Errorf("comments: expected numeric id argument: %w", err)
		}
		s, err := loadItem(ctx, id)
		if err != nil {
			return fmt.Errorf("loading story: %w", err)
		}
		fmt.Printf(`%v (%vpts, %vcms)
	by %v
--------------------`, s.Title, s.Score, s.Descendants, s.By)
		ss := make(map[int]string)
		var l sync.Mutex
		errchan := make(chan error)
		for _, k := range s.Kids {
			lk := k
			go func() {
				c, err := comments(ctx, lk)
				if err != nil {
					errchan<-fmt.Errorf("loading comment %v: %w", lk, err)
				}
				l.Lock()
				defer l.Unlock()
				ss[lk] = c
				errchan <- nil
			}()
		}
		for range s.Kids {
			err := <- errchan
			if err != nil {
				return fmt.Errorf("subcomment: %w", err)
			}
		}
		for _, k := range s.Kids {
			fmt.Println(ss[k])
		}
	}
	return nil
}


// Top loads and prints the top 25 HN stories.
func top(ctx context.Context) error {
	var stories []int
	err := requests.URL(base + "/v0/topstories.json").
		Get().
		ToJSON(&stories).
		Fetch(ctx)
	if err != nil {
		return fmt.Errorf("fetch top stories: %w", err)
	}
	stories = stories[:25]
	type result struct {
		story item
		// Error will be set if the goroutine fetching this story failed for some reason.
		err error
		// The position of this story in the top stories list
		idx int
	}
	itemsChan := make(chan result)
	for idx, id := range stories {
		lid := id
		lidx := idx
		go func() {
			s, err := loadItem(ctx, lid)
			if err != nil {
				itemsChan <- result{err: fmt.Errorf("fetch story %v: %w", lid, err)}
				return
			}
			itemsChan <- result{story: s, idx: lidx}
		}()
	}

	items := make([]item, len(stories))
	for range stories {
		s := <- itemsChan
		items[s.idx] = s.story
	}
	close(itemsChan)
	for idx, i := range items {
		fmt.Printf(`%v.  %v
%vpts	by %v
		%v comments: exec://hn/comments/%v
		%v
`, idx+1, i.Title, i.Score, i.By, i.Descendants, i.Id, i.Url)
	}
	return nil
}

var converter = md.NewConverter("", true, nil)

// comments loads the comments for the given id, formats and returns them.
func comments(ctx context.Context, id int) (string, error) {
	// first we must load the story, so we can begin to load its comments.
	s, err := loadItem(ctx, id)
	if err != nil {
		return "", fmt.Errorf("fetch story: %w", err)
	}
	var out strings.Builder
	t := s.Text
	t = strings.ReplaceAll(t, "<p>", "\n\n")
	t, err = converter.ConvertString(t)
	if err != nil {
		return "", fmt.Errorf("parse comment: %w", err)
	}
	_, _ = out.WriteString(fmt.Sprintf(`
>>>>- by %v (
%v`, s.By, t))

	type result struct {s string; err error; idx int}
	resultChan := make(chan result)
	for i, id := range s.Kids {
		li := i
		lid := id
		go func(){
			ss, err := comments(ctx, lid)
			if err != nil {
				resultChan <- result{err: fmt.Errorf("item %v: %w", lid, err)}
				return
			}
			resultChan <- result{s: ss, idx: li}
		}()
	}
	subs := make([]string, len(s.Kids))
	for range s.Kids {
		r := <- resultChan
		if r.err != nil {
			return "", fmt.Errorf("load sub comment: %w", r.err)
		}
		subs[r.idx] = r.s
	}
	for _, ss := range subs {
		_, _ = out.WriteString("\n")
		_, _ = out.WriteString(indent(ss))
	}
	_, _ = out.WriteString("\n)")
	return out.String(), nil
}

// Indents each line of the given string and returns it.
func indent(s string) string {
	ls := strings.Split(s, "\n")
	for i, l := range ls {
		ls[i] = fmt.Sprintf("\t%v", l)
	}
	return strings.Join(ls, "\n")
}

// loads the given item id.
func loadItem(ctx context.Context, id int) (item, error) {
	var i item
	err := requests.URL(fmt.Sprintf("%v/v0/item/%v.json", base, id)).
		Get().
		ToJSON(&i).
		Fetch(ctx)
	if err != nil {
		return i, fmt.Errorf("fetch: %w", err)
	}
	return i, nil
}