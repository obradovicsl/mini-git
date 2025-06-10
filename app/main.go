package main

import (
	"bytes"
	"compress/zlib"
	"crypto/sha1"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strconv"
)

// Usage: your_program.sh <command> <arg1> <arg2> ...
func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "usage: mygit <command> [<args>...]\n")
		os.Exit(1)
	}

	switch command := os.Args[1]; command {
	case "init":
		for _, dir := range []string{".git", ".git/objects", ".git/refs"} {
			if err := os.MkdirAll(dir, 0755); err != nil {
				fmt.Fprintf(os.Stderr, "Error creating directory: %s\n", err)
				os.Exit(1)
			}
		}

		headFileContents := []byte("ref: refs/heads/main\n")
		if err := os.WriteFile(".git/HEAD", headFileContents, 0644); err != nil {
			fmt.Fprintf(os.Stderr, "Error writing file: %s\n", err)
			os.Exit(1)
		}

		fmt.Println("Initialized git directory")
	case "cat-file":
		objectPath, flag, err := parseCatFile(os.Args[2:])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error while getting object path: %s\n", err)
			os.Exit(1)
		}

		object_bytes, err := getUncompressedObject(objectPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error while decompressing object: %s\n", err)
			os.Exit(1)
		}

		err = printObjectData(object_bytes, flag)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error while reading object: %s\n", err)
			os.Exit(1)
		}
	case "hash-object":
		objectPath, flag, err := parseHashObject(os.Args[2:])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error while parssing args: %s\n", err)
			os.Exit(1)
		}
		objectContent, objectSize, err := readObjectContent(objectPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error while reading object: %s\n", err)
			os.Exit(1)
		}
		objectBytes := generateObject("blob", objectSize, objectContent)
		hasher := sha1.New()
		hasher.Write(objectBytes)
		hash := hasher.Sum(nil)

		compressedObject, err := compressObject(objectBytes)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error while compresing object: %s\n", err)
			os.Exit(1)
		}

		switch flag {
		case "-w":
			err := writeObject(hash, compressedObject)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error while writting the object: %s\n", err)
				os.Exit(1)
			}
		}
		fmt.Printf("%x\n", hash)

	default:
		fmt.Fprintf(os.Stderr, "Unknown command %s\n", command)
		os.Exit(1)
	}
}

func parseCatFile(args []string) (string, string, error) {
	// all objects are inside .git/objects directory
	// we'll use first two characters of objectName to find directory of object
	if len(args) != 2 {
		return "", "", fmt.Errorf("use: git cat-file <flag> <object_name>")
	}

	objectFlag, objectName := args[0], args[1]

	if objectFlag != "-t" && objectFlag != "-s" && objectFlag != "-p" {
		return "", "", fmt.Errorf("use: <flag> shold be -t or -s or -p")
	}

	dir := objectName[:2]
	file := objectName[2:]
	objectPath := filepath.Join(".git", "objects", dir, file)

	if _, err := os.Stat(objectPath); os.IsNotExist(err) {
		return "", "", fmt.Errorf("object %s not found", objectName)
	}

	return objectPath, objectFlag, nil
}

func parseHashObject(args []string) (string, string, error) {
	if len(args) != 1 && len(args) != 2 {
		return "", "", fmt.Errorf("use: git hash-object <flag> <object_path>")
	}

	var flag string
	var path string
	if len(args) == 2 {
		flag = args[0]
		path = args[1]
	} else if len(args) == 1 {
		flag = ""
		path = args[0]
	}

	// check path

	if _, err := os.Stat(path); os.IsNotExist(err) {
		return "", "", fmt.Errorf("object on %s path not found", path)
	}

	return path, flag, nil
}

func getUncompressedObject(objectPath string) ([]byte, error) {
	data, err := os.ReadFile(objectPath)
	if err != nil {
		return nil, err
	}

	reader, err := zlib.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	decompressed, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}

	return decompressed, nil
}

func printObjectData(objectBytes []byte, flag string) error {
	nullIndex := bytes.IndexByte(objectBytes, 0)
	if nullIndex == -1 {
		return fmt.Errorf("invalid object format: no null byte")
	}

	header := objectBytes[:nullIndex]    // <type> <size>
	content := objectBytes[nullIndex+1:] // <content>

	parts := bytes.SplitN(header, []byte(" "), 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid object header")
	}

	objectType := string(parts[0])
	objectSize := string(parts[1])

	switch flag {
	case "-t":
		// Print type of the object
		fmt.Printf(objectType)

	case "-s":
		// Print size of the object
		fmt.Printf(objectSize)

	case "-p":
		// Print content of the object
		fmt.Printf(string(content))
	}

	return nil
}

func readObjectContent(path string) ([]byte, int, error) {
	fileData, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, err
	}

	return fileData, len(fileData), nil
}

func generateObject(objectType string, objectSize int, objectContent []byte) []byte {
	header := objectType + " " + strconv.Itoa(objectSize)
	headerNull := append([]byte(header), byte(0))
	return append(headerNull, objectContent...)
}

func compressObject(object []byte) ([]byte, error) {
	var b bytes.Buffer
	zw := zlib.NewWriter(&b)

	_, err := zw.Write(object)
	if err != nil {
		return nil, fmt.Errorf("Error while compressing the object")
	}

	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("Error while closing writter")
	}

	return b.Bytes(), nil
}

func writeObject(hash, object []byte) error {
	hashString := fmt.Sprintf("%x", hash)

	dirName := hashString[:2]
	fileName := hashString[2:]

	dirPath := path.Join(".git/objects", dirName)
	if err := os.MkdirAll(dirPath, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %v", err)
	}

	fullPath := path.Join(dirPath, fileName)
	if err := os.WriteFile(fullPath, object, 0644); err != nil {
		return fmt.Errorf("failed to write object file: %v", err)
	}

	return nil
}
