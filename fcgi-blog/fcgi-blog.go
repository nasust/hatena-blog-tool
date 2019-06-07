package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"net/http/fcgi"
	"net/url"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"github.com/PuerkitoBio/goquery"
	"github.com/antonholmquist/jason"
	"gopkg.in/gographics/imagick.v1/imagick"
)

const IMAGE_DIR = "/home/mori/www/blogimage/"
const URL_PREFIX = "http://nasust.hatenablog.com/"

func Exists(name string) bool {
	_, err := os.Stat(name)
	return !os.IsNotExist(err)
}

func handler(writer http.ResponseWriter, req *http.Request) {
	url := req.FormValue("url")

	if strings.HasPrefix(url, URL_PREFIX) == false {
		writer.WriteHeader(http.StatusNotFound)
		fmt.Fprintln(writer, "Not Found: ", url)
		return
	}
	width := req.FormValue("width")
	height := req.FormValue("height")

	imageFileName := strings.Replace(url, URL_PREFIX, "", 1)
	imageFileName = strings.Replace(imageFileName, "/", "-", -1)

	imageFileName = imageFileName + "-width=" + width + "-height=" + height + ".jpeg"

	if Exists(IMAGE_DIR + imageFileName) {
		file, err := os.Open(IMAGE_DIR + imageFileName)
		if err != nil {
			writer.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintln(writer, err)
			return
		}
		defer file.Close()

		reader := bufio.NewReader(file)
		writer.Header().Set("Content-Type", "image/jpeg")
		writer.WriteHeader(http.StatusOK)

		_, err = io.Copy(writer, reader)

		if err != nil {
			writer.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintln(writer, err)
			return
		}

	} else {
		doc, err := goquery.NewDocument(url)
		if err != nil {
			writer.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintln(writer, err)
			return
		}

		selection := doc.Find("meta[property='og:image']")
		if selection.Length() == 0 {
			writer.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintln(writer, "not found og image")
			return
		}
		selection = selection.First()
		content, exists := selection.Attr("content")
		if exists == false {
			writer.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintln(writer, "not found og image content")
			return
		}

		response, err := http.Get(content)
		if err != nil {
			writer.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintln(writer, err)
			return
		}
		defer response.Body.Close()

		byteArray, _ := ioutil.ReadAll(response.Body)

		var imageBytes []byte

		if width == "auto" && height == "auto" {
			imageBytes = byteArray
		} else {
			imagick.Initialize()
			defer imagick.Terminate()

			nw := imagick.NewMagickWand()
			defer nw.Destroy()

			err = nw.ReadImageBlob(byteArray)
			if err != nil {
				writer.WriteHeader(http.StatusInternalServerError)
				fmt.Fprintln(writer, err)
				return
			}

			imageWidth := nw.GetImageWidth()
			imageHeight := nw.GetImageHeight()

			if height == "auto" {
				parseWidth, err := strconv.ParseUint(width, 10, 64)
				if err != nil {
					writer.WriteHeader(http.StatusInternalServerError)
					fmt.Fprintln(writer, err)
					return
				}
				scaleWidth := uint(parseWidth)
				if scaleWidth > 1024 {
					writer.WriteHeader(http.StatusInternalServerError)
					fmt.Fprintln(writer, "width <= 1024")
					return
				}
				scale := float64(scaleWidth) / float64(imageWidth)
				scaleHeight := uint(float64(imageHeight) * scale)
				nw.ResizeImage(scaleWidth, scaleHeight, imagick.FILTER_LANCZOS2_SHARP, 1.0)
				imageBytes = nw.GetImageBlob()
			} else {
				imageBytes = byteArray
			}

			err = nw.WriteImage(IMAGE_DIR + imageFileName)
			if err != nil {
				writer.WriteHeader(http.StatusInternalServerError)
				fmt.Println(writer, err)
				return
			}

		}

		writer.Header().Set("Content-Type", "image/jpeg")
		writer.WriteHeader(http.StatusOK)
		writer.Write(imageBytes)

	}
}

func handlerStar(writer http.ResponseWriter, req *http.Request) {
	urls := req.FormValue("urls")
	callback := req.FormValue("callback")

	if urls == "" || callback == "" {
		writer.WriteHeader(http.StatusInternalServerError)
		fmt.Println(writer, "param error")
		return
	}

	urlList := strings.Split(urls, ",")

	var uri string = ""

	for i, queryUrl := range urlList {
		if strings.HasPrefix(queryUrl, "http://nasust.hatenablog.com") == false {
			writer.WriteHeader(http.StatusInternalServerError)
			fmt.Println(writer, "http://nasust.hatenablog.com only")
			return
		}

		uri += "uri=" + url.QueryEscape(queryUrl)
		if i < len(urlList)-1 {
			uri += "&"
		}
	}

	response, err := http.Get("http://s.hatena.com/entry.json?" + uri)
	if err != nil {
		writer.WriteHeader(http.StatusInternalServerError)
		fmt.Println(writer, err)
		return
	}
	defer response.Body.Close()

	starsJson, err := jason.NewObjectFromReader(response.Body)
	if err != nil {
		writer.WriteHeader(http.StatusInternalServerError)
		fmt.Println(writer, err)
		return
	}

	starCountMap := map[string]interface{}{}

	entries, _ := starsJson.GetObjectArray("entries")
	for _, entry := range entries {

		var starCount int
		stars, _ := entry.GetObjectArray("stars")
		for _, star := range stars {
			count, err := star.GetInt64("count")
			if err == nil {
				starCount += int(count)
			} else {
				starCount += 1
			}
		}

		coloredStars, _ := entry.GetObjectArray("colored_stars")
		for _, coloredStar := range coloredStars {
			stars, _ := coloredStar.GetObjectArray("stars")
			for _, star := range stars {
				count, err := star.GetInt64("count")
				if err == nil {
					starCount += int(count)
				} else {
					starCount += 1
				}
			}

		}

		entryUri, _ := entry.GetString("uri")
		starCountMap[entryUri] = starCount
	}

	jsonBytes, _ := json.MarshalIndent(starCountMap, "", "    ")
	jsonp := callback + "(" + string(jsonBytes) + ");"

	writer.Header().Set("Content-Type", "application/javascript")
	writer.WriteHeader(http.StatusOK)
	fmt.Fprint(writer, jsonp)

}

func handlerBlur(writer http.ResponseWriter, req *http.Request) {
	url := req.FormValue("url")

	if strings.HasPrefix(url, URL_PREFIX) == false {
		writer.WriteHeader(http.StatusNotFound)
		fmt.Fprintln(writer, "Not Found: ", url)
		return
	}

	imageFileName := strings.Replace(url, URL_PREFIX, "", 1)
	imageFileName = strings.Replace(imageFileName, "/", "-", -1)

	imageFileName = imageFileName + "-blur.png"

	if Exists(IMAGE_DIR + imageFileName) {
		file, err := os.Open(IMAGE_DIR + imageFileName)
		if err != nil {
			writer.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintln(writer, err)
			return
		}
		defer file.Close()

		reader := bufio.NewReader(file)
		writer.Header().Set("Content-Type", "image/png")
		writer.WriteHeader(http.StatusOK)

		_, err = io.Copy(writer, reader)

		if err != nil {
			writer.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintln(writer, err)
			return
		}

	} else {
		doc, err := goquery.NewDocument(url)
		if err != nil {
			writer.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintln(writer, err)
			return
		}

		selection := doc.Find("meta[property='og:image']")
		if selection.Length() == 0 {
			writer.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintln(writer, "not found og image")
			return
		}
		selection = selection.First()
		content, exists := selection.Attr("content")
		if exists == false {
			writer.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintln(writer, "not found og image content")
			return
		}

		response, err := http.Get(content)
		if err != nil {
			writer.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintln(writer, err)
			return
		}
		defer response.Body.Close()

		byteArray, _ := ioutil.ReadAll(response.Body)

		var imageBytes []byte

		nw := imagick.NewMagickWand()
		defer nw.Destroy()

		mask := imagick.NewMagickWand()
		defer mask.Destroy()

		err = nw.ReadImageBlob(byteArray)
		if err != nil {
			writer.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintln(writer, err)
			return
		}

		err = mask.ReadImage(IMAGE_DIR + "mask.png")
		if err != nil {
			writer.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintln(writer, err)
			return
		}

		err = nw.SetFormat("png32")
		if err != nil {
			writer.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintln(writer, err)
			return
		}

		err = nw.SetImageMatte(true)
		if err != nil {
			writer.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintln(writer, err)
			return
		}

		imageWidth := nw.GetImageWidth()
		imageHeight := nw.GetImageHeight()

		newWidth := imageWidth / 3
		newHeight := imageHeight / 3

		nw.ResizeImage(newWidth, newHeight, imagick.FILTER_LANCZOS2_SHARP, 1.0)
		mask.ResizeImage(newWidth, newHeight, imagick.FILTER_LANCZOS2_SHARP, 1.0)

		nw.BlurImage(32, 4)

		nw.CompositeImage(mask, imagick.COMPOSITE_OP_DST_IN, 0, 0)

		imageBytes = nw.GetImageBlob()

		err = nw.WriteImage(IMAGE_DIR + imageFileName)
		if err != nil {
			writer.WriteHeader(http.StatusInternalServerError)
			fmt.Println(writer, err)
			return
		}

		writer.Header().Set("Content-Type", "image/png")
		writer.WriteHeader(http.StatusOK)
		writer.Write(imageBytes)
	}

}

var colorMap sync.Map

func handlerColorAvarage(writer http.ResponseWriter, req *http.Request) {
	url := req.FormValue("url")
	callback := req.FormValue("callback")

	if strings.HasPrefix(url, URL_PREFIX) == false || callback == "" {
		writer.WriteHeader(http.StatusNotFound)
		fmt.Fprintln(writer, "Not Found: ", url)
		return
	}

	if value, ok := colorMap.Load(url); ok {
		if jsonString, ok := value.(string); ok {
			jsonp := callback + "(" + jsonString + ");"
			writer.Header().Set("Content-Type", "application/javascript")
			writer.WriteHeader(http.StatusOK)
			fmt.Fprint(writer, jsonp)
			return
		}
	}

	doc, err := goquery.NewDocument(url)
	if err != nil {
		writer.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintln(writer, err)
		return
	}

	selection := doc.Find("meta[property='og:image']")
	if selection.Length() == 0 {
		writer.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintln(writer, "not found og image")
		return
	}
	selection = selection.First()
	content, exists := selection.Attr("content")
	if exists == false {
		writer.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintln(writer, "not found og image content")
		return
	}

	response, err := http.Get(content)
	if err != nil {
		writer.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintln(writer, err)
		return
	}
	defer response.Body.Close()

	byteArray, _ := ioutil.ReadAll(response.Body)

	nw := imagick.NewMagickWand()
	defer nw.Destroy()

	err = nw.ReadImageBlob(byteArray)
	if err != nil {
		writer.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintln(writer, err)
		return
	}

	nw.ResizeImage(1, 1, imagick.FILTER_LANCZOS2_SHARP, 1.0)

	color, _ := nw.GetImagePixelColor(0, 0)
	colorString := color.GetColorAsNormalizedString()
	colorCount := color.GetColorCount()

	jsonMap := map[string]interface{}{}
	jsonMap["color"] = colorString
	jsonMap["count"] = colorCount

	jsonBytes, _ := json.MarshalIndent(jsonMap, "", "    ")
	jsonString := string(jsonBytes)

	colorMap.Store(url, jsonString)

	jsonp := callback + "(" + jsonString + ");"

	writer.Header().Set("Content-Type", "application/javascript")
	writer.WriteHeader(http.StatusOK)
	fmt.Fprint(writer, jsonp)
}

func main() {
	runtime.GOMAXPROCS(runtime.NumCPU() * 10)

	imagick.Initialize()
	defer imagick.Terminate()

	l, err := net.Listen("unix", "/home/mori/var/run/go-fcgi.sock")
	if err != nil {
		fmt.Println("listen error: ", err)
	}

	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, os.Interrupt, os.Kill, syscall.SIGTERM)
	go func(c chan os.Signal) {
		sig := <-c
		log.Printf("Caught signal %s: shutting down.", sig)
		l.Close()
		os.Exit(0)
	}(sigc)

	http.HandleFunc("/fcgi/blog-image", handler)
	http.HandleFunc("/fcgi/blog-image-blur", handlerBlur)
	http.HandleFunc("/fcgi/color-avarage", handlerColorAvarage)
	http.HandleFunc("/fcgi/star", handlerStar)

	fcgi.Serve(l, nil)
}
