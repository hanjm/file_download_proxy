package main

import (
	"fmt"
	"path"
	"regexp"
	"strings"
	"time"
)

var safe_filename_regexp = regexp.MustCompile(`[\w\d.]+`)

func get_safe_filename(url string) string {
	_, filename_in_url := path.Split(url)
	filename := strings.Join(safe_filename_regexp.FindAllString(filename_in_url, -1), "")
	if len_of_filename := len(filename); len_of_filename > 50 {
		filename = filename[len_of_filename-50 : len_of_filename]
	}
	file_ext := path.Ext(filename)
	return fmt.Sprintf("%s-%v%s", strings.Replace(filename, file_ext, "", -1), time.Now().Unix(), file_ext)

}

func get_human_size_string(byte_size int64) string {
	units := []string{"B", "KB", "MB", "GB", "TB", "EB"}
	index := 0
	byte_size_float := float64(byte_size)
	for ; byte_size_float > 1024; index += 1 {
		byte_size_float /= 1024
	}
	return fmt.Sprintf("%.2f %s", byte_size_float, units[index])
}
