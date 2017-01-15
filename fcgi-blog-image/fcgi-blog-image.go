package main

import (
	"bufio"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"net/http/fcgi"
	"os"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
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

		reader := bufio.NewReader(file)
		writer.Header().Set("Content-Type", "image/jpeg")

		_, err = io.Copy(writer, reader)

		if err != nil {
			writer.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintln(writer, err)
			return
		}
		writer.WriteHeader(http.StatusOK)
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
		writer.Write(imageBytes)
		writer.WriteHeader(http.StatusOK)

	}
}

func main() {
	l, err := net.Listen("unix", "/var/run/go-fcgi.sock")
	if err != nil {
		fmt.Println("listen error: ", err)
	}
	http.HandleFunc("/fcgi/blog-image", handler)
	fcgi.Serve(l, nil)
}
