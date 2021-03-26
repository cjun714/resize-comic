package main

import "C"
import (
	"archive/tar"
	"bytes"
	"fmt"
	"io"
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

func pack(src, targetDir string) error {
	log.I("resize:", src)

	baseName := filepath.Base(src)
	ext := filepath.Ext(baseName)
	newName := strings.TrimSuffix(baseName, ext) + "[resized]" + ".cbt"
	target := filepath.Join(targetDir, newName)

	return packArc(src, target)
}

func packArc(src, target string) error {
	ar, e := unarr.NewArchive(src)
	if e != nil {
		return e
	}
	defer ar.Close()

	f, e := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0666)
	if e != nil {
		return e
	}

	wr := tar.NewWriter(f)
	defer wr.Close()

	var lock sync.Mutex
	var wg sync.WaitGroup
	for e == nil {
		if e = ar.Entry(); e != nil {
			break
		}
		name := filepath.Base(ar.Name())

		// TODO unarr lib ignore dir entry in archive file
		if !isImage(name) {
			continue
		}

		// TODO unrar doesn't checksum
		data, e := ar.ReadAll()
		if e != nil {
			log.E("extract file failed:", name, ", error:", e)
			continue
		}

		wg.Add(1)
		go func(fname string, modtime time.Time, fdata []byte) {
			defer wg.Done()

			pixPtr, w, h, comps, e := stb.LoadBytes(fdata)
			if e != nil {
				log.E("stb decode failed:", fname)
				return
			}
			defer stb.Free(pixPtr)
			pix := C.GoBytes(unsafe.Pointer(pixPtr), C.int(w*h*comps))

			cfg := webp.NewConfig(webp.SET_DRAWING, quality)
			if h > height {
				cfg.SetResize(0, height)
			}
			var buf bytes.Buffer
			if e := webp.EncodeBytes(&buf, pix, w, h, comps, cfg); e != nil {
				log.E("encode webp failed:", fname, " , error:", e)
			}

			fname = replaceSuffix(fname, ".webp")
			hd := &tar.Header{
				Name:    fname,
				Mode:    int64(0666),
				Size:    int64(buf.Len()),
				ModTime: modtime,
			}
			lock.Lock()
			if e := wr.WriteHeader(hd); e != nil {
				log.E("write header failed:", src, ", name:", fname, ", error:", e)
				return
			}
			if _, e := wr.Write(buf.Bytes()); e != nil {
				log.E("write data failed:", src, ", name:", fname, ", error:", e)
				return
			}
			lock.Unlock()
		}(name, ar.ModTime(), data)
	}
	wg.Wait()

	if e != nil && e != io.EOF {
		return e
	}

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
