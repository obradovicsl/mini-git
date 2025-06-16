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
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

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
			fmt.Fprintf(os.Stderr, "Error with init command: %s\n", err)
			os.Exit(1)
		}
		fmt.Println("Initialized git directory")
	case "cat-file":
		// Extract cmd arguments
		objectHash, flag, err := parseCatCmdArgs(os.Args[2:])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error while parsing cat-file command: %s\n", err)
			os.Exit(1)
		}

		// Based on given SHA1 hash, read object from .git/objects
		objType, objSize, objContent, err := readObjectFromHash(objectHash)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error while decompressing object: %s\n", err)
			os.Exit(1)
		}

		// Based on provided flag, print required data
		switch flag {
		case "-t":
			// Print type of the object
			fmt.Println(objType)

		case "-s":
			// Print size of the object
			fmt.Println(objSize)

		case "-p":
			// Print content of the object
			fmt.Println(string(objContent))
		}
	case "hash-object":
		// Extract cmd arguments
		objectPath, flag, err := parseHashObjectCmdArgs(os.Args[2:])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error while parssing hash-object command args: %s\n", err)
			os.Exit(1)
		}

		// Read file from provided path
		objectContent, _, err := readObjectFromPath(objectPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error while reading object: %s\n", err)
			os.Exit(1)
		}

		// Generate object (<type> <size>\0<content>) and hashes it
		objectBytes := generateObjectByte("blob", objectContent)
		hash := hashObject(objectBytes)

		// If -w flag is provided - write object to .git/objects
		switch flag {
		case "-w":
			_, err := writeObject(objectBytes)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error while writting the object: %s\n", err)
				os.Exit(1)
			}
		}

		// Print objects hash
		fmt.Printf("%x\n", hash)
	case "ls-tree":
		// Extract cmd arguments
		treeHash, flag, err := parseLsTreeCmdArgs(os.Args[2:])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error while getting tree path: %s\n", err)
			os.Exit(1)
		}

		// Get tree content (from .git/objects/....)
		_, _, treeContent, err := readObjectFromHash(treeHash)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error while decompressing tree: %s\n", err)
			os.Exit(1)
		}

		// Print the tree content
		err = printTreeData(treeContent, flag)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error while reading tree: %s\n", err)
			os.Exit(1)
		}
	case "write-tree":
		// Load the whole staging area (.git/index entries)
		indexEntries, err := readGitIndex()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error while reading .git/index: %s\n", err)
			os.Exit(1)
		}

		// Make a tree struct for optimizing tree creation - without this, some object generations would be repeated
		directoryRoot := makeDirTree(indexEntries)

		// Iterate over created Tree, create required blob objects and tree objects, and populate directory nodes with hash values
		err = dfsTreeCreation(directoryRoot)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error while generating tree object: %s\n", err)
			os.Exit(1)
		}
		// printTree(directoryRoot)

		// Print root dir hash
		fmt.Printf("%x\n", directoryRoot.Hash)
	case "commit-tree":
		// Extract cmd arguments
		treeHash, commitMessage, parentHash, err := parseCommitTreeCmdArgs(os.Args[2:])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error while parsing args: %s\n", err)
			os.Exit(1)
		}

		// Create content for commit object and use it to generate commit object
		commitContent := createCommitContent(treeHash, commitMessage, parentHash)
		objectBytes := generateObjectByte("commit", commitContent)

		// Generate hash, compress object and write it to .git/objects/
		hash, err := writeObject(objectBytes)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error while writting the commit: %s\n", err)
			os.Exit(1)
		}
		// Print objects hash
		fmt.Printf("%x\n", hash)
	case "clone":
		// Extract URL and Directory names from cmd args
		remoteUrl, directoryName, err := parseCloneCmdArgs(os.Args[2:])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error while parssing args: %s\n", err)
			os.Exit(1)
		}

		// Create a directory (with name that was provided)
		err = os.MkdirAll(directoryName, 0755)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error while creating %s directory: %s\n", directoryName, err)
			os.Exit(1)
		}
		// Change to the new directory created to run all the other file creations
		err = os.Chdir(directoryName)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error while changing to %s directory: %s\n", directoryName, err)
			os.Exit(1)
		}
		// Initialize repository inside newly created directory
		initRepo()

		fmt.Printf("Cloning from %s into %s\n", remoteUrl, directoryName)

		// Send GET req to github to fetch refs (file formated as pkt-line - contains all refs that remote repository (GitHub) knows)
		// We want only the commit object that is pointed by main HEAD
		refs, err := fetchRefs(remoteUrl)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error while fetching refs: %v:\n", err)
			os.Exit(1)
		}

		hashHead, _, err := extractHeadFromRefs(refs)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error while extracting HEAD from refs: %v:\n", err)
			os.Exit(1)
		}
		fmt.Printf("HEAD sha1 hash: %s\n", hashHead)

		// git-upload-pack REQUEST

		// following GitHub Smart HTTP protocol make want-have request
		request := buildUploadPackRequest(hashHead)
		// send want-have request to get .pack file
		packData, err := sendUploadPackRequest(remoteUrl, request)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error during git-upload-pack request: %v\n", err)
			os.Exit(1)
		}

		// Parse pack file (extract objects - blob, trees, commits, deltified)
		objects, err := parsePackFile(packData)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error while parsing packfile: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Successfully read %d objects:\n", len(objects))

		// Write all objects to .git/objects
		err = writePackObjects(objects)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error while writing objects: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Successfully wrote %d objects:\n", len(objects))

		err = renderFilesFromCommit(hashHead)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error while rendering object files: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("Successfully cloned repository:\n")

	default:
		fmt.Fprintf(os.Stderr, "Unknown command %s\n", command)
		os.Exit(1)
	}
}

// Initialize .git repo with .git/objects .git/refs directories and .git/index .git/HEAD files
func initRepo() error {
	for _, dir := range []string{".git", ".git/objects", ".git/refs"} {
		if err := os.Mkdir(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory: %v", err)
		}
	}
	headFileContents := []byte("ref: refs/heads/master\n")
	if err := os.WriteFile(".git/HEAD", headFileContents, 0644); err != nil {
		return fmt.Errorf("failed to write HEAD file: %v", err)
	}

	err := createEmptyIndex()
	if err != nil {
		return fmt.Errorf("failed to create .git/index: %v", err)
	}
	return nil
}

// Create empty .git/index file
func createEmptyIndex() error {
	// Index v2 header:
	// 4 bytes: signature ("DIRC")
	// 4 bytes: version (2)
	// 4 bytes: entry count (0)
	header := make([]byte, 12)
	copy(header[0:4], []byte("DIRC"))
	binary.BigEndian.PutUint32(header[4:8], 2)  // version 2
	binary.BigEndian.PutUint32(header[8:12], 0) // 0 entries

	// SHA1 checksum of the content (excluding checksum itself)
	hash := sha1.Sum(header)

	// Append checksum to end
	full := append(header, hash[:]...)

	// Write to .git/index
	return os.WriteFile(".git/index", full, 0644)
}

// Read object from given SHA1 hash - returns ObjectType (blob/tree/commit), ObjectLen (in bytes), ObjectContent (byte array)
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

// Compress given object using zlib
func compressObject(object []byte) ([]byte, error) {
	var b bytes.Buffer
	zw := zlib.NewWriter(&b)

	_, err := zw.Write(object)
	if err != nil {
		return nil, fmt.Errorf("failed to compress the object")
	}

	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("failed to close writter")
	}

	return b.Bytes(), nil
}

// Creates Object Hash using SHA1 function
func hashObject(objectBytes []byte) []byte {
	hasher := sha1.New()
	hasher.Write(objectBytes)
	hash := hasher.Sum(nil)
	return hash
}

// Checks does object exists, and read its content
func readObjectFromPath(path string) ([]byte, int, error) {
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

// Print tree data based on provided Tree Object Content and flag
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

// Generate object with header and content (<type> <size>\0<content>) with provided type and content
func generateObjectByte(objectType string, objectContent []byte) []byte {
	header := objectType + " " + strconv.Itoa(len(objectContent))
	headerNull := append([]byte(header), byte(0))
	return append(headerNull, objectContent...)
}

// Takes in raw objet bytes, creates hash using SHA1, compress and write the object,
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

// Read .git/index file to retrieve all entries from it - returns IndexEntry array - used for write-tree command to write everything from staging area (.git/index)
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

// Creates Tree struct based on provided IndexEntries from .git/index
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

// Insert object on right place in the tree based on path - recursive
func insertInTree(root *TreeNode, path string, entry *IndexEntry) {
	// Get path parts by string splitting
	pathParts := strings.Split(path, "/")

	// Next path will not include current dir (if provided path is /app/src/text.txt - nextPath would be /src/text.txt)
	nextPath := strings.Join(pathParts[1:], "/")

	// If directory already exists - no need to create it
	if _, ok := root.Children[pathParts[0]]; ok {
		insertInTree(root.Children[pathParts[0]], nextPath, entry)
		return
	}

	// Create new TreeNode and populate it
	newNode := &TreeNode{
		Children: make(map[string]*TreeNode),
		Name:     pathParts[0],
	}

	// If path only has 1 element, that means that we are at 'leaf node' - STOPPING POINT in recursion
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

// Traverse down the Tree using DFS and prints each node name, hash and mode
func printTree(root *TreeNode) {
	if root == nil {
		return
	}

	for _, child := range root.Children {
		printTree(child)
	}
	fmt.Printf("Name: %s, hash: %x, mode: %s\n", root.Name, root.Hash)
}

// It will recursively create deepest subdirectories first, and then move up...
func dfsTreeCreation(root *TreeNode) error {

	// If it is a file - can't go deeper
	if len(root.Children) == 0 {
		return nil
	}

	// Create each subdirectory first
	for _, child := range root.Children {
		if child.IsDir {
			if err := dfsTreeCreation(child); err != nil {
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
	// Tree content will consist of its children
	treeContent := createTreeContent(root.Children)
	//
	treeByteObject := generateObjectByte("tree", treeContent)
	hash, err := writeObject(treeByteObject)
	if err != nil {
		return nil, err
	}
	return hash, nil
}

// Iterates over childer hashMap and creates tree content (for each child: <mode> <type> <sha1_hash> <name>)
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

// Creates a content for commit object with provided treeHash, commitMessage and parentHash - it uses hardcoded vals for username and email
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

///////////////////////////// CLONE //////////////////////////////////////////

// Sends HTTP GET request on /info/refs?service=git-upload-pack URL to get refs file.
func fetchRefs(remoteUrl string) ([]byte, error) {
	refsUrl := fmt.Sprintf("%s/info/refs?service=git-upload-pack", remoteUrl)

	resp, err := http.Get(refsUrl)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch refs: %v", err)
	}

	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %v", err)
	}

	return body, nil
}

// Extracts HEAD sha1 hash, and capabilities from refs file
func extractHeadFromRefs(byteRefs []byte) (string, string, error) {
	refs, capabilities, err := parseRefs(byteRefs)
	if err != nil {
		return "", "", err
	}

	return refs["HEAD"], capabilities, nil
}

// Parse refs file, and make hashMap out of it
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

// Build have-want request body
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

// Writes one line to Writer in pkt-line format
func writePktLine(w io.Writer, line string) {
	length := len(line) + 4
	fmt.Fprintf(w, "%04x%s", length, line)
}

// Sends HTTP request to /git-upload-pack, to retrieve .pack file
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

// Parse pack file - header (version and obj size) and content (objects), and extract all object from it
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

// Parse object header - retrieve obj size, obj type and number of used bytes
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

// Parse DELTA_OFS offset
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

// Read and decompress the whole Zlib object - returns object and number of used bytes
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

// Write object to .git/objects
func writeObjectWithType(content []byte, objectType ObjectType) ([]byte, error) {
	object := generateObjectByte(objectType.String(), content)
	// Write to disk
	hash, err := writeObject(object)
	if err != nil {
		return nil, err
	}
	return hash, nil
}

// Takes a list of objects, and write them
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

// Writes one DELTA_REF object
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

// Read var-length (if MSB == 1, then it has to read the next byte - the process repeats until it reads a byte with MSB == 0)
func parseDeltaHeader(objectData []byte) (int, int, int) {
	read := 0
	srcSize, used := parseDeltaSize(objectData)
	read += used
	targetSize, used := parseDeltaSize(objectData[read:])
	read += used
	return srcSize, targetSize, read
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

// Takes base object, and delta object, then apply COPY and INSERT instructions from delta object
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

// Generate all files from provided branch
func renderFilesFromCommit(branchHash string) error {
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

// Render the whole tree recursively 
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