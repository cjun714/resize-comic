package main

import (
	"archive/tar"
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/cjun714/go-image/webp"
	"github.com/gen2brain/go-unarr"
)

func init() {
	runtime.GOMAXPROCS(runtime.NumCPU())
}

var quality = 80

func main() {
	src := os.Args[1]
	targetDir := filepath.Dir(src)

	if len(os.Args) >= 3 {
		targetDir = os.Args[2]
	}

	start := time.Now()

	if fileExist(src) { // if src is file
		if e := pack(src, targetDir); e != nil {
			panic(e)
		}
		duration := time.Since(start)
		fmt.Println("cost: ", duration)

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
			fmt.Printf("convert failed, file: %s, error: %s\n", fPath, e)
		}

		return nil
	})

	duration := time.Since(start)
	fmt.Println("cost: ", duration)

	if e != nil {
		panic(e)
	}
}

func pack(src, targetDir string) error {
	fmt.Println("resize:", src)

	baseName := filepath.Base(src)
	ext := filepath.Ext(baseName)
	newName := strings.TrimSuffix(baseName, ext) + ".cbt"
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
	for ; e == nil; e = ar.Entry() {
		name := filepath.Base(ar.Name())

		// TODO unarr lib ignore dir entry in archive file
		if !isImage(name) {
			continue
		}

		// TODO unrar doesn't checksum
		data, e := ar.ReadAll()
		if e != nil {
			fmt.Printf("read file %s failed in %s, error:%s\n", name, src, e)
			continue
		}

		wg.Add(1)
		go func(fname string, modtime time.Time, content []byte) {
			defer wg.Done()

			cfg, e := webp.ConfigPreset(webp.PRESET_DRAWING, quality)
			if e != nil {
				fmt.Printf("config WebP encoder failed, error:%s\n", e)
				return
			}
			cfg.SetResizeHeight(1440)

			var buf bytes.Buffer
			if e := webp.EncodeBytes(bufio.NewWriter(&buf), content, cfg); e != nil {
				fmt.Printf("encode webp failed, file:%s, name:%s, error:%s\n",
					src, fname, e)
			}

			fname = replaceSuffix(fname, ".webp")
			h := &tar.Header{
				Name:    fname,
				Mode:    int64(0666),
				Size:    int64(buf.Len()),
				ModTime: modtime,
			}
			lock.Lock()
			if e := wr.WriteHeader(h); e != nil {
				fmt.Printf("write .cbt header failed, file:%s, name:%s, error:%s\n",
					src, fname, e)
				return
			}
			if _, e := wr.Write(buf.Bytes()); e != nil {
				fmt.Printf("write .cbt content failed, file:%s, name:%s, error:%s\n",
					src, fname, e)
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
