package main

import (
	"bufio"
	"bytes"
	"compress/zlib"
	"crypto/sha1"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type ObjectType int

func (objType ObjectType) String() string {
	switch objType {
	case OBJ_TREE:
		return "tree"
	case OBJ_COMMIT:
		return "commit"
	case OBJ_BLOB:
		return "blob"
	case OBJ_TAG:
		return "tag"
	case OBJ_OFS_DELTA:
		return "ofs_delta"
	case OBJ_REF_DELTA:
		return "ref_delta"
	default:
		return ""
	}
}

func ObjectTypeFromString(s string) (ObjectType, error) {
	switch s {
	case "tree":
		return OBJ_TREE, nil
	case "commit":
		return OBJ_COMMIT, nil
	case "blob":
		return OBJ_BLOB, nil
	case "tag":
		return OBJ_TAG, nil
	default:
		return 0, fmt.Errorf("unknown ObjectType: " + s)
	}
}

const (
	OBJ_COMMIT    = 1
	OBJ_TREE      = 2
	OBJ_BLOB      = 3
	OBJ_TAG       = 4
	OBJ_OFS_DELTA = 6
	OBJ_REF_DELTA = 7
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

type GitObject struct {
	Type        ObjectType
	Data        []byte
	BaseObjHash string
	Size        uint64
}

// Usage: your_program.sh <command> <arg1> <arg2> ...
func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "usage: mygit <command> [<args>...]\n")
		os.Exit(1)
	}

	switch command := os.Args[1]; command {
	case "init":
		err := initRepo()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error with init: %s\n", err)
			os.Exit(1)
		}
		fmt.Println("Initialized git directory")
	case "cat-file":
		objectHash, flag, err := parseCatFile(os.Args[2:])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error while getting object path: %s\n", err)
			os.Exit(1)
		}

		objType, objSize, objContent, err := readObjectFromHash(objectHash)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error while decompressing object: %s\n", err)
			os.Exit(1)
		}

		err = printObjectData(objType, objSize, objContent, flag)
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
		objectContent, _, err := readObjectContent(objectPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error while reading object: %s\n", err)
			os.Exit(1)
		}

		objectBytes := generateObjectByte("blob", objectContent)
		hash := hashObject(objectBytes)

		switch flag {
		case "-w":
			_, err := writeObject(objectBytes)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error while writting the object: %s\n", err)
				os.Exit(1)
			}
		}
		fmt.Printf("%x\n", hash)

	case "ls-tree":
		treeHash, flag, err := parseLsTree(os.Args[2:])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error while getting tree path: %s\n", err)
			os.Exit(1)
		}

		_, _, treeContent, err := readObjectFromHash(treeHash)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error while decompressing tree: %s\n", err)
			os.Exit(1)
		}

		err = printTreeData(treeContent, flag)
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
			fmt.Fprintf(os.Stderr, "Error while parsing args: %s\n", err)
			os.Exit(1)
		}

		commitContent := createCommitContent(treeHash, commitMessage, parentHash)
		objectBytes := generateObjectByte("commit", commitContent)

		hash, err := writeObject(objectBytes)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error while writting the commit: %s\n", err)
			os.Exit(1)
		}
		fmt.Printf("%x\n", hash)

	case "clone":
		// Get repo_url an dir_name from args
		remoteUrl, directoryName, err := parseClone(os.Args[2:])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error while parssing args: %s\n", err)
			os.Exit(1)
		}
		err = os.MkdirAll(directoryName, 0755)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error while creating %s directory: %s\n", directoryName, err)
			os.Exit(1)
		}
		// change to the new directory created to run all the other file creations

		err = os.Chdir(directoryName)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error while changing to %s directory: %s\n", directoryName, err)
			os.Exit(1)
		}

		initRepo()

		fmt.Printf("Cloning from %s into %s\n", remoteUrl, directoryName)

		// Send GET req to github
		hashHead, _, err := fetchRefs(remoteUrl)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error while fetching refs: %v:\n", err)
			os.Exit(1)
		}

		// git-upload-pack request

		// make want-have request
		request := buildUploadPackRequest(hashHead)

		// send request
		packData, err := sendUploadPackRequest(remoteUrl, request)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error during git-upload-pack request: %v\n", err)
			os.Exit(1)
		}

		// Parse pack file (extract objects - blob, trees, commits)
		objects, err := parsePackFile(packData)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error while parsing packfile: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("Successfully read %d objects:\n ", len(objects))
		// Write objects to .git/objects
		err = writePackObjects(objects)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error while writing objects: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("Successfully wrote %d objects:\n ", len(objects))

		err = renderFiles(hashHead)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error while rendering object files: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("Successfully cloned repository:\n ")

	default:
		fmt.Fprintf(os.Stderr, "Unknown command %s\n", command)
		os.Exit(1)
	}
}

func initRepo() error {
	for _, dir := range []string{".git", ".git/objects", ".git/refs"} {
		if err := os.Mkdir(dir, 0755); err != nil {
			return fmt.Errorf("Error creating directory: %s\n", err)
		}
	}
	headFileContents := []byte("ref: refs/heads/master\n")
	if err := os.WriteFile(".git/HEAD", headFileContents, 0644); err != nil {
		fmt.Errorf("Error writing file: %s\n", err)
	}
	return nil
}

/////////////////////////COMMAND PARSER////////////////////////////////////////////////////////////////////

func parseCatFile(args []string) (string, string, error) {
	if len(args) != 2 {
		return "", "", fmt.Errorf("use: git cat-file <flag> <object_hash>")
	}

	objectFlag, objectHash := args[0], args[1]

	if objectFlag != "-t" && objectFlag != "-s" && objectFlag != "-p" {
		return "", "", fmt.Errorf("use: <flag> shold be -t or -s or -p")
	}

	return objectHash, objectFlag, nil
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

	return path, flag, nil
}

func parseLsTree(args []string) (string, string, error) {
	if len(args) != 1 && len(args) != 2 {
		return "", "", fmt.Errorf("use: git ls-tree <flag> <tree_path>")
	}

	var flag string
	var treeHash string
	if len(args) == 2 {
		flag = args[0]
		treeHash = args[1]
	} else if len(args) == 1 {
		flag = ""
		treeHash = args[0]
	}

	return treeHash, flag, nil
}

func parseCommitTree(args []string) (string, string, string, error) {
	if len(args) != 3 && len(args) != 5 {
		return "", "", "", fmt.Errorf("use: git commit-tree <HASH> -p <HASH> -m <message>")
	}

	var message string
	var treeHash string
	var parentSHA string
	if len(args) == 3 {
		treeHash = args[0]
		message = args[2]
		parentSHA = ""
	} else if len(args) == 5 {
		treeHash = args[0]
		parentSHA = args[2]
		message = args[4]
	}

	return treeHash, message, parentSHA, nil
}

func parseClone(args []string) (string, string, error) {
	if len(args) != 2 {
		return "", "", fmt.Errorf("use: git clone <URL> <some_dir>")
	}

	var url string
	var directory string

	url = args[0]
	directory = args[1]

	return url, directory, nil
}

////////////////////////////////////////////////////////////////////////////////////////////////////////////

func readObjectFromHash(objectHash string) (string, string, []byte, error) {
	dir := objectHash[:2]
	file := objectHash[2:]
	objectPath := filepath.Join(".git", "objects", dir, file)

	if _, err := os.Stat(objectPath); os.IsNotExist(err) {
		return "", "", nil, fmt.Errorf("object on %s path not found", objectPath)
	}

	data, err := os.ReadFile(objectPath)
	if err != nil {
		return "", "", nil, err
	}

	reader, err := zlib.NewReader(bytes.NewReader(data))
	if err != nil {
		return "", "", nil, err
	}
	defer reader.Close()

	decompressed, err := io.ReadAll(reader)
	if err != nil {
		return "", "", nil, err
	}

	header, body, _ := bytes.Cut(decompressed, []byte{0x00})

	parts := strings.Split(string(header), " ")
	objType, objSize := parts[0], parts[1]

	return objType, objSize, body, nil
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

func printObjectData(objectType, objectSize string, objectContent []byte, flag string) error {
	switch flag {
	case "-t":
		// Print type of the object
		fmt.Printf(objectType)

	case "-s":
		// Print size of the object
		fmt.Printf(objectSize)

	case "-p":
		// Print content of the object
		fmt.Printf(string(objectContent))
	}

	return nil
}

func printTreeData(objectContent []byte, flag string) error {
	i := 0
	for i < len(objectContent) {
		nullIndex := bytes.IndexByte(objectContent[i:], 0)
		if nullIndex == -1 {
			return fmt.Errorf("malformed tree entry")
		}

		entryHeader := objectContent[i : i+nullIndex]
		parts := bytes.SplitN(entryHeader, []byte(" "), 2)
		mode := string(parts[0])
		name := string(parts[1])

		i += nullIndex + 1
		if i+20 > len(objectContent) {
			return fmt.Errorf("unexpected end of SHA")
		}

		shaBytes := objectContent[i : i+20]
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
	// check path
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, 0, fmt.Errorf("object on %s path not found", path)
	}

	fileData, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, err
	}

	return fileData, len(fileData), nil
}

func generateObjectByte(objectType string, objectContent []byte) []byte {
	header := objectType + " " + strconv.Itoa(len(objectContent))
	headerNull := append([]byte(header), byte(0))
	return append(headerNull, objectContent...)
}

// Takes in raw objet bytes, creates hash using SHA1, compress and write the object
func writeObject(object []byte) ([]byte, error) {

	hash := hashObject(object)
	compressedObject, err := compressObject(object)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error while compresing commit: %s\n", err)
		os.Exit(1)
	}

	hashString := fmt.Sprintf("%x", hash)

	dirName := hashString[:2]
	fileName := hashString[2:]

	dirPath := path.Join(".git/objects", dirName)
	if err := os.MkdirAll(dirPath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create directory: %v", err)
	}

	fullPath := path.Join(dirPath, fileName)

	if _, err := os.Stat(fullPath); err == nil {
		return hash, nil
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("error checking object file: %v", err)
	}

	if err := os.WriteFile(fullPath, compressedObject, 0644); err != nil {
		return nil, fmt.Errorf("failed to write object file: %v", err)
	}

	return hash, nil
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
	treeByteObject := generateObjectByte("tree", treeContent)
	hash, err := writeObject(treeByteObject)
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

func fetchRefs(remoteUrl string) (string, string, error) {
	refsUrl := fmt.Sprintf("%s/info/refs?service=git-upload-pack", remoteUrl)

	resp, err := http.Get(refsUrl)
	if err != nil {
		return "", "", fmt.Errorf("failed to fetch refs: %v", err)
	}

	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", "", fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", fmt.Errorf("failed to read response body: %v", err)
	}

	refs, capabilities, err := parseRefs(body)
	if err != nil {
		return "", "", err
	}

	return refs["HEAD"], capabilities, nil
}

func parseRefs(body []byte) (map[string]string, string, error) {
	refs := make(map[string]string)
	var capabilities string

	scanner := bufio.NewScanner(bytes.NewReader(body))
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) > 4 && string(line[4:]) != "" && !bytes.HasPrefix(line[4:], []byte("#")) {
			// Split the line by null byte

			parts := bytes.Split(line[4:], []byte{0x00})
			var caps []byte
			if len(parts) > 1 {
				caps = parts[1]
				capabilities = string(caps)
			}

			if len(parts) > 0 {
				chunk2 := parts[0]

				// Check if the string ends with "HEAD", then remove the first 4 characters
				if len(chunk2) > 4 && bytes.HasSuffix(chunk2, []byte("HEAD")) {
					chunk2 = chunk2[4:]
				}

				// Split by space to form the chunk array
				chunk := bytes.Split(chunk2, []byte(" "))
				if len(chunk) >= 2 {
					// Decode chunk[0] and chunk[1] and store them in refs map
					refs[string(chunk[1])] = string(chunk[0])
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		fmt.Println("Error scanning response body:", err)
	}

	return refs, capabilities, nil
}

func buildUploadPackRequest(hash string) []byte {
	var buf bytes.Buffer

	// First line: "want <hash> <capabilities>\n"
	wantLine := fmt.Sprintf("want %s\n", hash)
	writePktLine(&buf, wantLine)

	buf.WriteString("0000")
	// Second line - done - we don't want anything more
	writePktLine(&buf, "done\n")

	buf.WriteString("0000")

	return buf.Bytes()
}

func writePktLine(w io.Writer, line string) {
	length := len(line) + 4
	fmt.Fprintf(w, "%04x%s", length, line)
}

func sendUploadPackRequest(remoteUrl string, request []byte) ([]byte, error) {
	url := remoteUrl + "/git-upload-pack"

	client := &http.Client{}
	req, err := http.NewRequest("POST", url, bytes.NewReader(request))
	if err != nil {
		return nil, fmt.Errorf("failed to create POST request: %v", err)
	}

	// REQUIRED headers for smart HTTP upload-pack request
	req.Header.Set("Content-Type", "application/x-git-upload-pack-request")
	req.Header.Set("Accept", "application/x-git-upload-pack-result")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %v", err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	packData, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %v", err)
	}

	return packData, nil
}

func parsePackFile(data []byte) ([]GitObject, error) {

	// end of .pack file a check sum (last 20 bytes) - we don't need that now
	data = data[:len(data)-20]

	offset := bytes.Index(data, []byte("PACK")) + 4
	version := binary.BigEndian.Uint32(data[offset : offset+4])
	offset += 4
	numObjects := binary.BigEndian.Uint32(data[offset : offset+4])
	offset += 4

	objects := make([]GitObject, 0, numObjects)

	fmt.Printf("Version: %d, %d objects\n", version, numObjects)

	for i := 0; i < int(numObjects); i++ {

		_, used, objType, err := parseObjectHeader(data[offset:])
		if err != nil {
			return nil, fmt.Errorf("failed to parse object header: %v", err)
		}
		offset += used
		var baseObjHash string
		if objType == OBJ_REF_DELTA {
			baseObjHash = hex.EncodeToString(data[offset : offset+20])
			offset += 20
		} else if objType == OBJ_OFS_DELTA {
			_, ofsLen := parseDeltaOffset(data[offset:])
			offset += ofsLen
		}

		zlibStart := offset
		decompressed, used, err := readZlibObject(data[zlibStart:])
		if err != nil {
			return nil, fmt.Errorf("failed to read obj delta content at %d: %w", zlibStart, err)
		}
		offset += used

		objects = append(objects, GitObject{
			Type:        objType,
			BaseObjHash: baseObjHash,
			Data:        decompressed,
		})
	}

	return objects, nil
}

func parseObjectHeader(data []byte) (uint64, int, ObjectType, error) {
	used := 0
	// Header is usually the first byte
	byteData := data[used]
	used++

	// Object type is always (6-4 bits)
	objectType := ObjectType((byteData >> 4) & 0x7)
	size := uint64(byteData & 0xF)
	shift := 4
	// If MSB == 1, we have to look the next byte
	for byteData&0x80 != 0 {
		// MSB == 1
		if len(data) <= used || 64 <= shift {
			return 0, 0, 0, fmt.Errorf("bad object header")
		}
		byteData = data[used]
		used++
		size += uint64(byteData&0x7F) << shift
		shift += 7
	}

	return size, used, objectType, nil

}

func readZlibObject(pack []byte) ([]byte, int, error) {
	reader := bytes.NewReader(pack)
	r, err := zlib.NewReader(reader)
	if err != nil {
		return nil, 0, err
	}
	defer r.Close()

	decompData, err := io.ReadAll(r)
	if err != nil {
		return nil, 0, err
	}

	used := int(reader.Size()) - reader.Len()

	return decompData, used, nil
}

func parseDeltaSize(packFile []byte) (int, int) {
	size := packFile[0] & 0b01111111
	index, off := 1, 7

	for packFile[index-1]&0b10000000 > 0 { // Check if MSB is set
		size = size | (packFile[index]&0b01111111)<<off
		off += 7
		index += 1
	}

	// this index is the same as the used bytes

	return int(size), index
}

func writeObjectWithType(content []byte, objectType ObjectType) ([]byte, error) {
	object := generateObjectByte(objectType.String(), content)
	// Write to disk
	hash, err := writeObject(object)
	if err != nil {
		return nil, err
	}
	return hash, nil
}

func parseDeltaOffset(data []byte) (val uint64, used int) {
	b := data[0]
	val = uint64(b & 0x7F)
	used = 1
	for b&0x80 != 0 {
		b = data[used]
		val = (val+1)<<7 | uint64(b&0x7F)
		used++
	}
	return
}

func writePackObjects(objects []GitObject) error {

	for _, obj := range objects {
		if obj.Type == OBJ_BLOB || obj.Type == OBJ_COMMIT || obj.Type == OBJ_TREE || obj.Type == OBJ_TAG {
			_, err := writeObjectWithType(obj.Data, obj.Type)
			if err != nil {
				return fmt.Errorf("failed to write %s object: %v", string(obj.Type), err)
			}

		} else if obj.Type == OBJ_REF_DELTA {
			err := writeRefDeltaObject(obj)
			if err != nil {
				return fmt.Errorf("failed to write %s object: %v", string(obj.Type), err)
			}
		}
	}
	return nil
}

func writeRefDeltaObject(object GitObject) error {
	baseType, _, baseData, err := readObjectFromHash(object.BaseObjHash)
	if err != nil {
		return fmt.Errorf("failed to find base object for delta: %v", err)
	}
	read := 0
	_, _, used := parseDeltaHeader(object.Data)
	read += used
	deltaObject := object.Data[read:]

	reconstructed, err := applyDelta(baseData, deltaObject)
	if err != nil {
		return fmt.Errorf("failed to apply delta: %w", err)
	}

	objType, err := ObjectTypeFromString(baseType)
	if err != nil {
		return fmt.Errorf("unknown base object type: %v", err)
	}

	_, err = writeObjectWithType(reconstructed, objType)
	if err != nil {
		return fmt.Errorf("failed to write delta object: %v", err)
	}
	return nil
}

func parseDeltaHeader(objectData []byte) (int, int, int) {
	read := 0
	srcSize, used := parseDeltaSize(objectData)
	read += used
	targetSize, used := parseDeltaSize(objectData[read:])
	read += used
	return srcSize, targetSize, read
}

func applyDelta(base, delta []byte) ([]byte, error) {
	var result []byte
	i := 0
	for i < len(delta) {
		op := delta[i]
		i++
		if op&0x80 != 0 {
			// COPY from base
			var offset, size int
			// offset
			if op&0x01 != 0 {
				offset |= int(delta[i])
				i++
			}
			if op&0x02 != 0 {
				offset |= int(delta[i]) << 8
				i++
			}
			if op&0x04 != 0 {
				offset |= int(delta[i]) << 16
				i++
			}
			if op&0x08 != 0 {
				offset |= int(delta[i]) << 24
				i++
			}
			// size
			if op&0x10 != 0 {
				size |= int(delta[i])
				i++
			}
			if op&0x20 != 0 {
				size |= int(delta[i]) << 8
				i++
			}
			if op&0x40 != 0 {
				size |= int(delta[i]) << 16
				i++
			}
			if size == 0 {
				size = 0x10000
			} // default
			result = append(result, base[offset:offset+size]...)
		} else {
			// INSERT new bytes
			size := int(op)
			result = append(result, delta[i:i+size]...)
			i += size
		}
	}
	return result, nil
}

func renderFiles(branchHash string) error {
	_, _, commit, err := readObjectFromHash(branchHash)
	if err != nil {
		return fmt.Errorf("failed to read HEAD commit (%s): %v", branchHash, err)
	}

	lines := strings.Split(string(commit), "\n")
	var treeHash string

	for _, line := range lines {
		if strings.HasPrefix(line, "tree") {
			treeHash = strings.TrimPrefix(line, "tree ")
			break
		}
	}

	if treeHash == "" {
		return fmt.Errorf("tree hash not found in commit")
	}

	return renderTreeRecursive(treeHash, ".")
}

func renderTreeRecursive(treeHash, currentPath string) error {
	objType, _, content, err := readObjectFromHash(treeHash)
	if err != nil {
		return fmt.Errorf("cannot read tree %s: %v", treeHash, err)
	}
	if objType != "tree" {
		return fmt.Errorf("object %s is not a tree", treeHash)
	}

	// content of a directory (files/dirs)
	data := content
	i := 0
	for i < len(data) {
		// Read mode
		modeEnd := bytes.IndexByte(data[i:], ' ')
		mode := string(data[i : i+modeEnd])
		i += modeEnd + 1

		// Read name
		nameEnd := bytes.IndexByte(data[i:], 0)
		name := string(data[i : i+nameEnd])
		i += nameEnd + 1

		// Read SHA (20 bytes)
		shaBytes := data[i : i+20]
		objHash := hex.EncodeToString(shaBytes)
		i += 20

		fullPath := filepath.Join(currentPath, name)

		if mode == "40000" {
			// directory
			if err := os.MkdirAll(fullPath, 0755); err != nil {
				return err
			}
			if err := renderTreeRecursive(objHash, fullPath); err != nil {
				return err
			}
		} else {
			// blob (file)
			typ, _, blobContent, err := readObjectFromHash(objHash)
			if err != nil {
				return err
			}
			if typ != "blob" {
				return fmt.Errorf("expected blob, got %s", typ)
			}
			if err := os.WriteFile(fullPath, blobContent, 0644); err != nil {
				return err
			}
		}
	}

	return nil
}
