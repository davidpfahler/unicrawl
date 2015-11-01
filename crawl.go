package main

import (
	"bytes"
	"crypto/md5"
	"flag"
	"fmt"
	"github.com/PuerkitoBio/goquery"
	"github.com/aryann/difflib"
	"github.com/jaytaylor/html2text"
	"github.com/kardianos/osext"
	m "github.com/mailgun/mailgun-go"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"reflect"
	"strings"
)

func getExecFolder() string {
	folder, _ := osext.ExecutableFolder()
	return folder
}

func loadUrls() []string {
	dat, err := ioutil.ReadFile(getExecFolder() + "/urls.txt")
	if err != nil {
		log.Fatal(err)
	}
	lines := strings.Split(string(dat), "\n")

	// ignore empty lines
	var urls []string
	for _, line := range lines {
		if line != "" {
			urls = append(urls, line)
		}
	}
	return urls
}

func getMD5Hash(s string) string {
	h := md5.Sum([]byte(s))
	return fmt.Sprintf("%x", h)
}

func requestURL(url string) []byte {
	res, err := http.Get(url)
	if err != nil {
		log.Fatal(err)
	}
	defer res.Body.Close()

	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		log.Fatal(err)
	}
	return body
}

func getGoqueryDoc(body []byte) *goquery.Document {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		fmt.Println("goquery failed to create doc")
	}
	return doc
}

func sendEmail(gun m.Mailgun, address string, msg string, htmlMsg string) {
	message := gun.NewMessage("David Pfahler <djpfahler@gmail.com>", "[Monitor]: Uni-Webseite hat sich geändert", msg, address)
	message.SetHtml(htmlMsg)
	response, id, _ := gun.Send(message)
	fmt.Printf("Response ID: %s\n", id)
	fmt.Printf("Message from server: %s\n", response)
}

func loadEmailAddresses() []string {
	dat, err := ioutil.ReadFile(getExecFolder() + "/emails.txt")
	if err != nil {
		log.Fatal(err)
	}
	lines := strings.Split(string(dat), "\n")

	// ignore empty lines
	var emails []string
	for _, line := range lines {
		if line != "" {
			emails = append(emails, line)
		}
	}
	return emails
}

func sendEmails(gun m.Mailgun, msg string, htmlMsg string) {
	addresses := loadEmailAddresses()
	for _, address := range addresses {
		sendEmail(gun, address, msg, htmlMsg)
	}
}

func createMessage(diff []difflib.DiffRecord, url string) string {
	msg := fmt.Sprintf(`Die Uni-Webseite unter folgender URL hat sich verändert:
%s
Unten stehend findest Du eine Übersicht der Änderungen:

`, url)
	for _, record := range diff {
		switch record.Delta {
		case difflib.RightOnly:
			msg += fmt.Sprintf("+%s\n", record.Payload)
		case difflib.LeftOnly:
			msg += fmt.Sprintf("-%s\n", record.Payload)
		default:
			msg += record.Payload + "\n"
		}
	}
	return msg
}

func createHTMLMessage(diff []difflib.DiffRecord, url string) string {
	msg := fmt.Sprintf(`<p>Die Uni-Webseite unter folgender URL hat sich verändert:</p>
	<p><a href="%s">%s</a><p>
	<p>Unten stehend findest Du eine Übersicht der Änderungen:</p>`, url, url)
	for _, record := range diff {
		switch record.Delta {
		case difflib.RightOnly:
			msg += fmt.Sprintf("<p style='color: green'>%s</p>", record.Payload)
		case difflib.LeftOnly:
			msg += fmt.Sprintf("<p style='color: red'>%s</p>", record.Payload)
		default:
			msg += fmt.Sprintf("<p>%s</p>", record.Payload)
		}
	}
	return msg
}

func updateCache(filename string, body []byte) {
	writeErr := ioutil.WriteFile(filename, body, 0644)
	if writeErr != nil {
		log.Fatalf("Could not write to cache on path %s\n", filename)
	}
}

func main() {
	domain := flag.String("domain", "", "mailgun domain")
	privateKey := flag.String("private-key", "", "secret mailgun api key")
	publicKey := flag.String("public-key", "", "mailgun public api key")
	dry := flag.Bool("dry", false, "do not update cache & only print to stdout (no e-mail)")
	flag.Parse()

	if *domain == "" || *privateKey == "" || *publicKey == "" {
		log.Fatalln("domain, private-key and public-key flags are required")
	}

	gun := m.NewMailgun(*domain, *privateKey, *publicKey)

	urls := loadUrls()
	for _, url := range urls {
		body := requestURL(url)
		filename := getExecFolder() + "/cache/" + getMD5Hash(url) + ".html"

		cached, err := ioutil.ReadFile(filename)
		if err != nil {
			if *dry == false {
				updateCache(filename, body)
			}

			switch err := err.(type) {
			case *os.PathError:
				fmt.Printf("This URL will now be monitored: %s\n\n", url)
			default:
				log.Fatalf("Fatal errors type %T\n", err)
			}
		} else {
			if reflect.DeepEqual(cached, body) {
				fmt.Printf("This URL didn't change: %s\n\n", url)
			} else {
				cachedDoc := getGoqueryDoc(cached)
				currentDoc := getGoqueryDoc(body)
				cachedContent, _ := cachedDoc.Find("#content").Html()
				currentContent, _ := currentDoc.Find("#content").Html()

				if cachedContent == currentContent {
					fmt.Println("The website changed, but the content stayed the same.")
				} else {
					cachedText, _ := html2text.FromString(cachedContent)
					currentText, _ := html2text.FromString(currentContent)
					diff := difflib.Diff(strings.Split(cachedText, "\n"), strings.Split(currentText, "\n"))

					msg := createMessage(diff, url)
					htmlMsg := createHTMLMessage(diff, url)
					if *dry {
						fmt.Println(msg)
					} else {
						sendEmails(gun, msg, htmlMsg)
					}
				}
				if *dry == false {
					updateCache(filename, body)
				}
			}
		}
	}
}
