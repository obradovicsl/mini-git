package main

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"io"
	"os"
	"path/filepath"
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
		object_path, flag, err := getObjectPathAndFlag(os.Args[2:])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error while getting object path: ", err)
			os.Exit(1)
		}

		object_bytes, err := getUncompressedObject(object_path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error while decompressing object: ", err)
			os.Exit(1)
		}

		err = readObjectData(object_bytes, flag)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error while reading object: ", err)
			os.Exit(1)
		}

	default:
		fmt.Fprintf(os.Stderr, "Unknown command %s\n", command)
		os.Exit(1)
	}
}

func getObjectPathAndFlag(args []string) (string, string, error) {
	// all objects are inside .git/objects directory
	// we'll use first two characters of objectName to find directory of object
	if len(args) != 2 {
		return "", "", fmt.Errorf("use: git-cat <flag> <object_name>")
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

func readObjectData(objectBytes []byte, flag string) error {
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
