package main

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

const (
	numChunks = 1 // Количество потоков
	url0      = "http://ftp.altlinux.org/pub/distributions/ALTLinux/p10/branch/"
)

var url2 string
var pool []string

func loadPool() {
	var wg sync.WaitGroup
	numWorkers := 5
	ch := make(chan string, numWorkers)

	// Запуск воркеров
	for i := 0; i < numWorkers; i++ {
		go func() {
			for fileURL := range ch {
				loadFile(fileURL, &wg)
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

func loadFile(adr string, wgPool *sync.WaitGroup) {
	defer wgPool.Done()

	// Определяем размер файла
	resp, err := http.Head(adr)
	if err != nil {
		fmt.Println("Ошибка при получении заголовков:", err)
		return
	}
	defer resp.Body.Close()

	contentLengthStr := resp.Header.Get("Content-Length")
	if contentLengthStr == "" {
		fmt.Println("Ошибка: Content-Length отсутствует")
		return
	}

	contentLength, err := strconv.Atoi(contentLengthStr)
	if err != nil {
		fmt.Println("Ошибка получения размера файла:", err)
		return
	}

	chunkSize := contentLength / numChunks
	var wg sync.WaitGroup

	fileName := adr[strings.LastIndex(adr, "/")+1:]
	fileName, _ = url.QueryUnescape(fileName)
	file, err := os.Create(fileName)
	if err != nil {
		fmt.Println("Ошибка создания файла:", err)
		return
	}
	file.Close()

	// Загружаем файл частями в горутинах
	for i := 0; i < numChunks; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			start := i * chunkSize
			end := start + chunkSize - 1
			if i == numChunks-1 {
				end = contentLength - 1
			}

			req, err := http.NewRequest("GET", adr, nil)
			if err != nil {
				fmt.Println("Ошибка создания запроса:", err)
				return
			}
			req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end))

			client := &http.Client{}
			resp, err := client.Do(req)
			if err != nil {
				fmt.Println("Ошибка скачивания части:", err)
				return
			}
			defer resp.Body.Close()

			// Открываем файл для записи
			file, err := os.OpenFile(fileName, os.O_WRONLY, 0644)
			if err != nil {
				fmt.Println("Ошибка открытия файла:", err)
				return
			}
			defer file.Close()

			// Перемещаемся в нужное место
			_, err = file.Seek(int64(start), 0)
			if err != nil {
				fmt.Println("Ошибка seek:", err)
				return
			}

			_, err = io.Copy(file, resp.Body)
			if err != nil {
				fmt.Println("Ошибка записи в файл:", err)
			}
		}(i)
	}

	wg.Wait()
	fmt.Println("Файл успешно скачан:", fileName)
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

func parsing(str string) {
	re := regexp.MustCompile(`<a href="([^"]+)">.*?</a>\s+(\d{2}-[A-Za-z]{3}-\d{4} \d{2}:\d{2})\s+(\d+)`)

	matches := re.FindStringSubmatch(str)
	if matches != nil {
		fileName := matches[1]
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

func main() {
	fmt.Println("Выберите архитектуру")
	fmt.Println("1. x86_64")
	fmt.Println("2. noarch")

	var zn string
	fmt.Scan(&zn)
	if zn == "1" {
		fmt.Println("Выбрана архитектура x86_64")
		url2 = url0 + "x86_64/RPMS.classic/"
	} else if zn == "2" {
		fmt.Println("Выбрана архитектура noarch")
		url2 = url0 + "noarch/RPMS.classic/"
	} else {
		fmt.Println("Некорректный выбор")
		return
	}

	loadSp()
}
