package main

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	numChunks  = 4 // Увеличено для многопоточной загрузки
	numWorkers = 5
	url0       = "http://ftp.altlinux.org/pub/distributions/ALTLinux/p10/branch/"
)

var url2 string
var pool []string
var poolMutex sync.Mutex
var client = &http.Client{Timeout: 15 * time.Second} // Общий HTTP-клиент

type fileContext struct {
	wg            *sync.WaitGroup
	fileURL       string
	chunkSize     int
	contentLength int
	fileName      string
}

func loadPool() {
	var wg sync.WaitGroup
	ch := make(chan string, numWorkers)

	// Запуск воркеров
	for i := 0; i < numWorkers; i++ {
		go func() {
			for fileURL := range ch {
				fileCont := fileContext{wg: &wg, fileURL: fileURL}
				err := fileCont.initLoad()
				if err != nil {
					wg.Done()
					fmt.Println(err)
					continue
				} else {
					fileCont.loadChunks()
				}

			}
		}()
	}

	// Добавление заданий
	for _, fileURL := range pool {
		wg.Add(1)
		ch <- fileURL
	}

	close(ch)
	wg.Wait()
}

func parsing(str string) {
	re := regexp.MustCompile(`<a href="([^\"]+)">.*?</a>\s+(\d{2}-[A-Za-z]{3}-\d{4} \d{2}:\d{2})\s+(\d+)`)
	matches := re.FindStringSubmatch(str)
	if matches != nil {
		fileName := matches[1]
		fileName, _ = url.QueryUnescape(fileName)
		fileSizeStr := matches[3]

		infoSize, err := strconv.Atoi(fileSizeStr)
		if err != nil {
			fmt.Println("Ошибка преобразования размера файла:", err)
			return
		}

		path := url2 + fileName
		fileInfo, err := os.Stat(fileName)
		if err == nil {
			fileSize := fileInfo.Size()
			if int(fileSize) != infoSize {
				os.Remove(fileName)
				pool = append(pool, path)
			}
		} else {
			pool = append(pool, path)
		}
	} else {
		fmt.Println("Не удалось распарсить строку:", str)
	}
}

func (fc *fileContext) initLoad() error {
	resp, err := client.Head(fc.fileURL)
	if err != nil {
		return errors.New("ошибка при получении заголовков")
	}
	defer resp.Body.Close()

	contentLengthStr := resp.Header.Get("Content-Length")
	if contentLengthStr == "" {
		return errors.New("Content-Length отсутствует")
	}

	contentLength, err := strconv.Atoi(contentLengthStr)
	if err != nil {
		return errors.New("ошибка получения размера файла")
	}

	fc.contentLength = contentLength
	fc.chunkSize = contentLength / numChunks

	fc.fileName = fc.fileURL[strings.LastIndex(fc.fileURL, "/")+1:]
	fc.fileName, _ = url.QueryUnescape(fc.fileName)
	file, err := os.Create(fc.fileName)
	if err != nil {
		return errors.New("ошибка создания файла " + fc.fileName)
	}
	file.Close()
	return nil
}

func (fc *fileContext) loadChunks() {
	defer fc.wg.Done()
	var wg sync.WaitGroup

	for i := 0; i < numChunks; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			retries := 3
			for retry := 0; retry < retries; retry++ {
				start := i * fc.chunkSize
				end := start + fc.chunkSize - 1
				if i == numChunks-1 {
					end = fc.contentLength - 1
				}

				req, err := http.NewRequest("GET", fc.fileURL, nil)
				if err != nil {
					fmt.Printf("Ошибка создания запроса: %v\n", err)
					continue
				}
				req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end))

				resp, err := client.Do(req)
				if err != nil {
					fmt.Printf("Ошибка скачивания части: %v\n", err)
					continue
				}
				defer resp.Body.Close()

				file, err := os.OpenFile(fc.fileName, os.O_WRONLY, 0777)
				if err != nil {
					fmt.Printf("Ошибка открытия файла: %v\n", err)
					continue
				}
				defer file.Close()

				_, err = file.Seek(int64(start), 0)
				if err != nil {
					fmt.Printf("Ошибка seek: %v\n", err)
					continue
				}

				_, err = io.Copy(file, resp.Body)
				if err != nil {
					fmt.Printf("Ошибка записи в файл: %v\n", err)
					continue
				}

				break // Успешная загрузка, выходим из цикла повторных попыток
			}
		}(i)
	}
	wg.Wait()
	fmt.Println("Файл успешно скачан:", fc.fileName)
}

func loadSp() {
	resp, err := http.Get(url2)
	if err != nil {
		fmt.Println("Ошибка загрузки списка файлов:", err)
		return
	}
	defer resp.Body.Close()

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Println("Ошибка чтения тела ответа:", err)
		return
	}

	mas := strings.Split(string(b), "\n")
	for _, line := range mas {
		if strings.Contains(line, ".rpm") {
			parsing(line)
		}
	}

	loadPool()
}

func main() {
	fmt.Println("Выберите архитектуру")
	fmt.Println("1. x86_64")
	fmt.Println("2. noarch")

	var zn string
	fmt.Scan(&zn)
	switch zn {
	case "1":
		url2 = url0 + "x86_64/RPMS.classic/"
	case "2":
		url2 = url0 + "noarch/RPMS.classic/"
	default:
		fmt.Println("Некорректный выбор")
		return
	}

	loadSp()
}
