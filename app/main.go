package main

import (
	"bytes"
	"compress/zlib"
	"crypto/sha1"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type IndexEntry struct {
	Path string
	Hash []byte
	Mode uint32
}

type TreeNode struct {
	Name     string
	IsDir    bool
	Hash     []byte
	Mode     uint32
	Children map[string]*TreeNode
}

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

		object_bytes, err := decompressObject(objectPath)
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
		hash := hashObject(objectBytes)

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

	case "ls-tree":
		treePath, flag, err := parseLsTree(os.Args[2:])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error while getting tree path: %s\n", err)
			os.Exit(1)
		}

		treeBytes, err := decompressObject(treePath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error while decompressing tree: %s\n", err)
			os.Exit(1)
		}

		err = printTreeData(treeBytes, flag)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error while reading tree: %s\n", err)
			os.Exit(1)
		}
	case "write-tree":
		// Load entries from .git/index

		// JUST FOR CODECRAFTERS TESTS
		///////////////////////////////////////////////////////
		cmd := exec.Command("git", "add", ".")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Stdin = os.Stdin // ako treba interakcija

		err := cmd.Run()
		if err != nil {
			panic(err)
		}
		///////////////////////////////////////////////////////

		indexEntries, err := readGitIndex()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error while reading .git/index: %s\n", err)
			os.Exit(1)
		}

		// Make a tree struct out of these index entries
		directoryRoot := makeDirTree(indexEntries)

		// Create directories from dirRoot
		err = createObjects(directoryRoot)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error while generating string objects: %s\n", err)
			os.Exit(1)
		}
		// printTree(directoryRoot)

		// Print root dir hash
		fmt.Printf("%x\n", directoryRoot.Hash)
	case "commit-tree":
		treeHash, commitMessage, parentHash, err := parseCommitTree(os.Args[2:])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error while parssing args: %s\n", err)
			os.Exit(1)
		}

		commitContent := createCommitContent(treeHash, commitMessage, parentHash)
		objectBytes := generateObject("commit", len(commitContent), commitContent)
		hash := hashObject(objectBytes)

		compressedObject, err := compressObject(objectBytes)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error while compresing commit: %s\n", err)
			os.Exit(1)
		}

		err = writeObject(hash, compressedObject)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error while writting the commit: %s\n", err)
			os.Exit(1)
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

func parseLsTree(args []string) (string, string, error) {
	if len(args) != 1 && len(args) != 2 {
		return "", "", fmt.Errorf("use: git ls-tree <flag> <tree_path>")
	}

	var flag string
	var treeSHA string
	if len(args) == 2 {
		flag = args[0]
		treeSHA = args[1]
	} else if len(args) == 1 {
		flag = ""
		treeSHA = args[0]
	}

	dir := treeSHA[:2]
	file := treeSHA[2:]
	treePath := filepath.Join(".git", "objects", dir, file)

	if _, err := os.Stat(treePath); os.IsNotExist(err) {
		return "", "", fmt.Errorf("tree on %s path not found", treePath)
	}

	return treePath, flag, nil
}

func parseCommitTree(args []string) (string, string, string, error) {
	if len(args) != 3 && len(args) != 5 {
		return "", "", "", fmt.Errorf("use: git commit-tree <HASH> -p <HASH> -m <message>")
	}

	var message string
	var treeSHA string
	var parentSHA string
	if len(args) == 3 {
		treeSHA = args[0]
		message = args[2]
		parentSHA = ""
	} else if len(args) == 5 {
		treeSHA = args[0]
		parentSHA = args[2]
		message = args[4]
	}

	return treeSHA, message, parentSHA, nil
}

func decompressObject(objectPath string) ([]byte, error) {
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

func hashObject(objectBytes []byte) []byte {
	hasher := sha1.New()
	hasher.Write(objectBytes)
	hash := hasher.Sum(nil)
	return hash
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

func printTreeData(objectBytes []byte, flag string) error {
	nullIndex := bytes.IndexByte(objectBytes, 0)
	if nullIndex == -1 {
		return fmt.Errorf("invalid tree format: no null byte")
	}

	content := objectBytes[nullIndex+1:] // <content>
	i := 0
	for i < len(content) {
		nullIndex := bytes.IndexByte(content[i:], 0)
		if nullIndex == -1 {
			return fmt.Errorf("malformed tree entry")
		}

		entryHeader := content[i : i+nullIndex]
		parts := bytes.SplitN(entryHeader, []byte(" "), 2)
		mode := string(parts[0])
		name := string(parts[1])

		i += nullIndex + 1
		if i+20 > len(content) {
			return fmt.Errorf("unexpected end of SHA")
		}

		shaBytes := content[i : i+20]
		shaHex := fmt.Sprintf("%x", shaBytes)
		i += 20

		if flag == "--name-only" {
			fmt.Println(name)
		} else {
			fmt.Printf("%s %s %s\n", mode, shaHex, name)
		}
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

func writeObject(hash, object []byte) error {
	hashString := fmt.Sprintf("%x", hash)

	dirName := hashString[:2]
	fileName := hashString[2:]

	dirPath := path.Join(".git/objects", dirName)
	if err := os.MkdirAll(dirPath, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %v", err)
	}

	fullPath := path.Join(dirPath, fileName)

	if _, err := os.Stat(fullPath); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("error checking object file: %v", err)
	}

	if err := os.WriteFile(fullPath, object, 0644); err != nil {
		return fmt.Errorf("failed to write object file: %v", err)
	}

	return nil
}

func readGitIndex() ([]IndexEntry, error) {
	file, err := os.Open(".git/index")
	if err != nil {
		return nil, err
	}

	defer file.Close()

	header := make([]byte, 12)
	if _, err := io.ReadFull(file, header); err != nil {
		return nil, err
	}

	if string(header[:4]) != "DIRC" {
		return nil, fmt.Errorf("invalid index signature")
	}

	version := binary.BigEndian.Uint32(header[4:8])
	if version != 2 {
		return nil, fmt.Errorf("unsupported index version: %d", version)
	}

	entryCount := binary.BigEndian.Uint32(header[8:12])
	entries := make([]IndexEntry, 0, entryCount)

	for i := 0; i < int(entryCount); i++ {
		entryHeader := make([]byte, 62)
		if _, err := io.ReadFull(file, entryHeader); err != nil {
			return nil, fmt.Errorf("reading entry header: %w", err)
		}

		mode := binary.BigEndian.Uint32(entryHeader[24:28])
		hash := make([]byte, 20)
		copy(hash, entryHeader[40:60])

		flags := binary.BigEndian.Uint16(entryHeader[60:62])
		nameLen := int(flags & 0x0FFF)

		nameBytes := make([]byte, nameLen)
		if _, err := io.ReadFull(file, nameBytes); err != nil {
			return nil, fmt.Errorf("reading path: %w", err)
		}

		totalLen := 62 + nameLen
		padding := (8 - (totalLen % 8)) % 8
		if _, err := io.CopyN(io.Discard, file, int64(padding)); err != nil {
			return nil, fmt.Errorf("discarding padding: %w", err)
		}

		entry := IndexEntry{
			Path: string(nameBytes),
			Hash: hash,
			Mode: mode,
		}

		entries = append(entries, entry)
	}

	return entries, nil
}

func makeDirTree(indexEntries []IndexEntry) *TreeNode {
	root := &TreeNode{
		Children: make(map[string]*TreeNode),
		Mode:     40000,
	}

	root.Name = "root"
	root.IsDir = true

	for _, entry := range indexEntries {
		insertInTree(root, entry.Path, &entry)
	}

	return root
}

func insertInTree(root *TreeNode, path string, entry *IndexEntry) {
	pathParts := strings.Split(path, "/")
	nextPath := strings.Join(pathParts[1:], "/")

	if _, ok := root.Children[pathParts[0]]; ok {
		// If it already exist
		insertInTree(root.Children[pathParts[0]], nextPath, entry)
		return
	}
	newNode := &TreeNode{
		Children: make(map[string]*TreeNode),
		Name:     pathParts[0],
	}
	if len(pathParts) == 1 {
		newNode.Hash = entry.Hash
		newNode.IsDir = false
		newNode.Mode = entry.Mode
		root.Children[pathParts[0]] = newNode
		return
	}

	newNode.IsDir = true
	newNode.Mode = 40000
	root.Children[pathParts[0]] = newNode
	insertInTree(root.Children[pathParts[0]], nextPath, entry)
}

func printTree(root *TreeNode) {
	if root == nil {
		return
	}

	for _, child := range root.Children {
		printTree(child)
	}
	fmt.Printf("Name: %s, hash: %x, mode: %s\n", root.Name, root.Hash)
}

// DFS
func createObjects(root *TreeNode) error {

	// If it is a file - can't go deeper
	if len(root.Children) == 0 {
		return nil
	}

	// Create each subdirectory first
	for _, child := range root.Children {
		if child.IsDir {
			if err := createObjects(child); err != nil {
				return err
			}
		}
	}

	// At this moment, we know that each sub-file/dir is already created
	hash, err := createTree(root)
	if err != nil {
		return err
	}

	root.Hash = hash
	return nil
}

// Creates compressed tree object and return its hash
func createTree(root *TreeNode) ([]byte, error) {
	treeContent := createTreeContent(root.Children)
	treeByteObject := generateObject("tree", len(treeContent), treeContent)
	hash := hashObject(treeByteObject)
	compressedTree, err := compressObject(treeByteObject)
	if err != nil {
		return nil, err
	}
	err = writeObject(hash, compressedTree)
	if err != nil {
		return nil, err
	}
	return hash, nil
}

func createTreeContent(children map[string]*TreeNode) []byte {
	var content []byte
	var keys []string
	for name := range children {
		keys = append(keys, name)
	}
	sort.Strings(keys)

	for _, name := range keys {
		child := children[name]

		var modeStr string
		if child.IsDir {
			modeStr = "40000"
		} else {
			modeStr = fmt.Sprintf("%06o", child.Mode)
		}

		entryHeader := fmt.Sprintf("%s %s", modeStr, child.Name)
		content = append(content, []byte(entryHeader)...)
		content = append(content, 0)

		content = append(content, child.Hash[:]...)

	}

	return content
}

func createCommitContent(treeHash, commitMessage, parentHash string) []byte {
	authorName := "obradovicsl"
	authorEmail := "slobodanobradovic3@gmail.com"
	now := time.Now()
	timestamp := now.Unix()
	timezoneOffset := now.Format("-0700") // Git-style timezone

	content := ""
	content += fmt.Sprintf("tree %s\n", treeHash)
	if parentHash != "" {
		content += fmt.Sprintf("parent %s\n", parentHash)
	}

	content += fmt.Sprintf("author %s <%s> %d %s\n", authorName, authorEmail, timestamp, timezoneOffset)
	content += fmt.Sprintf("committer %s <%s> %d %s\n", authorName, authorEmail, timestamp, timezoneOffset)
	content += "\n"
	content += commitMessage
	content += "\n"

	return []byte(content)
}
