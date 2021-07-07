package main

import "C"
import (
	"archive/tar"
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/cjun714/glog/log"
	"github.com/cjun714/go-image-stb/stb"
	"github.com/cjun714/go-image/webp"
	"github.com/gen2brain/go-unarr"
)

func init() {
	runtime.GOMAXPROCS(runtime.NumCPU())
}

var quality = 85
var height = 1440

func main() {
	src := os.Args[1]
	targetDir := filepath.Dir(src)

	if len(os.Args) >= 3 {
		targetDir = os.Args[2]
	}

	if len(os.Args) >= 4 {
		q, e := strconv.Atoi(os.Args[3])
		if e == nil {
			quality = q
		}
	}

	if len(os.Args) >= 5 {
		h, e := strconv.Atoi(os.Args[4])
		if e == nil {
			height = h
		}
	}

	log.I("quality:", quality, " | height:", height)

	start := time.Now()

	if fileExist(src) { // if src is file
		if e := pack(src, targetDir); e != nil {
			panic(e)
		}
		duration := time.Since(start)
		log.I("cost: ", duration)

		return
	}

	if !dirExist(targetDir) {
		if e := os.MkdirAll(targetDir, 0666); e != nil {
			panic(e)
		}
	}

	if !dirExist(src) {
		panic("target path is invalid: " + src)
	}

	// if src is dir, walk through
	e := filepath.Walk(src, func(fPath string, info os.FileInfo, err error) error {
		if info.IsDir() {
			rel, _ := filepath.Rel(src, fPath)

			if rel == "." { // skip root src dir
				return nil
			}

			// create same sub dir in targetDir
			newDir := filepath.Join(targetDir, rel)
			e := os.MkdirAll(newDir, 0666)
			if e != nil {
				return e
			}

			return nil
		}

		if !isComic(fPath) { // skip non-comic files
			return nil
		}

		rel, _ := filepath.Rel(src, filepath.Dir(fPath))
		newDir := filepath.Join(targetDir, rel)
		if e := pack(fPath, newDir); e != nil {
			log.E("convert failed:", fPath, " , error:", e)
		}

		return nil
	})

	duration := time.Since(start)
	log.I("cost: ", duration)

	if e != nil {
		panic(e)
	}
}

type zipData struct {
	name    string
	data    []byte
	modTime time.Time
}

func readArc(src string) ([]zipData, error) {
	byts, e := ioutil.ReadFile(src)
	if e != nil {
		return nil, e
	}
	br := bytes.NewBuffer(byts)

	fileList := make([]zipData, 0, 1)
	ar, e := unarr.NewArchiveFromReader(br)
	if e != nil {
		return nil, e
	}

	for e == nil {
		if e = ar.Entry(); e != nil {
			if e != io.EOF {
				log.E(e)
				log.E("read entry failed, entry name:", ar.Name())
			}
			break
		}
		name := ar.Name()
		modTime := ar.ModTime()

		data, e := ar.ReadAll()
		if e != nil {
			return nil, e
		}
		fileList = append(fileList, zipData{name, data, modTime})
	}

	if e != nil && e != io.EOF {
		return nil, e
	}

	return fileList, nil
}

func pack(src, targetDir string) error {
	log.I("resize:", src)

	baseName := filepath.Base(src)
	ext := filepath.Ext(baseName)
	newName := strings.TrimSuffix(baseName, ext) + "[resized]" + ".cbt"
	target := filepath.Join(targetDir, newName)

	return packArc(src, target)
}

func packArc(src, target string) error {
	list, e := readArc(src)
	if e != nil {
		return e
	}

	f, e := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0666)
	if e != nil {
		return e
	}
	defer f.Close()

	wr := tar.NewWriter(f)
	defer wr.Close()

	var lock sync.Mutex
	var wg sync.WaitGroup

	for _, file := range list {
		if !isImage(file.name) {
			log.I("skip:", file.name)
			continue
		}

		wg.Add(1)
		go func(f zipData) {
			defer wg.Done()

			var pixPtr *uint8
			var w, h, comps int
			if isWebp(f.name) {
				// TODO
			} else {
				pixPtr, w, h, comps, e = stb.LoadBytes(f.data)
				if e != nil {
					log.E("stb decode failed:", f.name)
					return
				}
				defer stb.Free(pixPtr)
			}
			pix := C.GoBytes(unsafe.Pointer(pixPtr), C.int(w*h*comps))

			cfg := webp.NewConfig(webp.SET_DRAWING, quality)
			if h > height {
				cfg.SetResize(0, height)
			}
			var buf bytes.Buffer
			if e := webp.EncodeBytes(&buf, pix, w, h, comps, cfg); e != nil {
				log.E("encode webp failed:", f.name, " , error:", e)
			}

			fname := replaceSuffix(f.name, ".webp")
			head := &tar.Header{
				Name:    fname,
				Mode:    int64(0666),
				Size:    int64(buf.Len()),
				ModTime: f.modTime,
			}

			lock.Lock()
			if e := wr.WriteHeader(head); e != nil {
				log.E("write header failed:", src, ", name:", f.name, ", error:", e)
				return
			}
			if _, e := wr.Write(buf.Bytes()); e != nil {
				log.E("write data failed:", src, ", name:", f.name, ", error:", e)
				return
			}
			lock.Unlock()
		}(file)
	}
	wg.Wait()

	return nil
}

func isImage(name string) bool {
	ext := filepath.Ext(name)
	ext = strings.ToLower(ext)
	if ext == ".jpeg" || ext == ".jpg" || ext == ".png" || ext == ".webp" {
		return true
	}
	if ext == ".bmp" || ext == ".gif" || ext == ".tga" {
		fmt.Println(name)
		return true
	}

	return false
}

func isComic(name string) bool {
	ext := filepath.Ext(name)
	ext = strings.ToLower(ext)
	return ext == ".cbr" || ext == ".cbz" || ext == ".cbt" ||
		ext == ".rar" || ext == ".zip" || ext == ".tar"
}

func dirExist(dirName string) bool {
	info, err := os.Stat(dirName)
	if os.IsNotExist(err) {
		return false
	}
	return info.IsDir()
}

func fileExist(fileName string) bool {
	info, e := os.Stat(fileName)
	if os.IsNotExist(e) {
		return false
	}
	return !info.IsDir()
}

func replaceSuffix(str, ext string) string {
	oldExt := filepath.Ext(str)
	str = strings.TrimSuffix(str, oldExt)
	return str + ext
}

func isWebp(name string) bool {
	return false
}
