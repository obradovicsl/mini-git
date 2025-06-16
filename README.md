# Mini Git

This project is a minimalist reimplementation of Git's core functionality, built to deepen understanding of Git internals. 
Each challenge corresponds to a critical Git feature, from initialization to cloning a full repository. 
The goal is to build a functional Git-like version control system from scratch.

## Overview

This project was born out of a desire to truly understand how Git works under the hood. As developers, we use Git every day, often treating it as a black box. I wanted to break that box open and look inside—how does Git actually manage to store and retrieve so many versions of files efficiently? What are Git objects? How are they structured and linked?

Working through the challenges gave me hands-on experience with Git's internal architecture, particularly its object model and how data is stored in the `.git` directory. The most rewarding and fascinating part was implementing the `git clone` command. This stage involved reverse-engineering Git's Smart HTTP protocol, parsing references, handling pack files, and learning how Git saves space using techniques like var-length integers, Big-Endian encoding, and delta compression with REF_DELTA. Working with the pack file format was particularly challenging—it is so tightly packed and optimized that parsing it required stepping through byte by byte, manually decompressing objects as they appeared, and carefully reconstructing their structure based on minimal, often indirect metadata.

Throughout the process, I gained insight into how Git balances performance, storage optimization, and simplicity through clever data structures and file formats.

## Challenge 01 - Create .git directory

Implements `git init` to create a `.git` directory

* `objects/` – stores Git objects
* `refs/` – stores Git references
* `index` - file that contains the staging area (place where all changes goes after `git add` command)
* `HEAD` – file that references the current branch (`ref: refs/heads/main\n`)

Original version of `.git` directory contains a little more files: 

* `hooks/` – lifecycle scripts (code that can be run after certain events)
* `config` – repo configuration (txt file with repo config - author, file mode...)

---

## Challenge 02 - Read a Blob

Implements `git cat-file` command to inspect objects:

```bash
git cat-file <flag> <object_name>
```

**Flags:**

* `-t` type of the object
* `-s` size of the object
* `-p` pretty-print the content


**Steps**
1. Find object path: `.git/objects/<first_two_chars_of_sha1>/<remaining_chars>` 
    - The `object_name` is the SHA-1 hash of the **uncompressed** object (<type><size>\0<content>)
2. Chech if the object exitst at the expected path
3. Read the object file
4. Decompress using zlib - all git objects are zlib-compressed
5. Parse the object header and content
    - Header format: `<type> <size>\0`
    - After the null byte (`\0`), the remaining bytes are the actual content of the object.

 **More about objects:** [Here](refs/GitObjects.md)

## Challenge 03 - Create a blob object

Implements `git hash-object` to create and write blob objects:

```bash
git hash-object [-w] <file>
```

**Flags:**

* `-w` object will be written to .git/object

**Steps:**

* Read file content from the provided path
* Create header and content
  - `<type> <size> \0 <content>`
* Calculate SHA-1 over header+content
* Print the SHA-1 of the object
* If `-w`, compress with zlib and write to `.git/objects/<hash>`

---

## Challenge 04 - Read a tree object

Implements `git ls-tree` to read tree objects. Trees are used to store directory structures - it contains blob objects and other trees. [Check for more](./refs/GitObjects.md)


```bash
git ls-tree [--name-only] <tree_sha>
```

**Tree object structure:**

* Header: `tree <size>\0`
* Entries: `<mode> <name>\0<sha>`
* Modes: `100644`, `100755`, `120000`, `40000`

---

## Challenge 05 - Write a Tree Object

The `git write-tree` command creates a tree object from the current state of the [*staging area*](https://git-scm.com/about/staging-area) (the place where changes go when you run `git add`).

`git write-tree` command is an internal part of the `git commit` process. When you run `git commit`, Git first executes `write-tree` to generate a snapshot of the current working directory (as per the staging area), and then creates a `commit` object that contains:

* A pointer to the generated tree object (i.e. snapshot)
* Commit metadata (author name, time, message...)
* A reference to the parent commit (if any)

For this challenge, we’ll *not* implement `git add`. Instead, we’ll assume that all files in the working directory are already staged.



### Key Concept

The key to understanding `write-tree` is realizing that Git only looks at the *staging area* (the `.git/index` file) when generating tree objects. It doesn’t care about what’s in the working directory or about past commits — only the *currently staged files* matter.

For each staged file, Git:

* Analyzes its path
* Creates tree objects for each directory along that path
* Associates blobs (file contents) with filenames and modes


### The `.git/index` File

The `.git/index` file is a binary structure that holds the list of all staged files, their metadata, and the blob SHA1s that correspond to their content. When implementing `write-tree`, you must parse this file and generate the directory hierarchy from the list of entries.


### Tree Object Format

Tree objects map directories to files or subdirectories. Each entry includes:

```
<file mode> <file name>\0<20-byte SHA1>
```

Example of a tree object with one file:

```
100644 hello.txt\0<sha1_bytes>
```

For nested directories, the SHA1 points to another tree object.



### 💥 The Problem: Redundant Work

Let’s say you have these staged files:

```
src/app/main.go
src/app/network.go
src/bin/alpha.exec
```

If you process these linearly, naïvely:

* You’ll generate a tree for `src/app` when handling `main.go`
* Then regenerate (and possibly overwrite) it when handling `network.go`
* You’ll also touch `src/` multiple times as `app/` and `bin/` are updated separately

This leads to:

* Rebuilding tree directories multiple times
* Writing identical tree objects multiple times
* Complexity and potential bugs in linking subtrees


### ✅ The Correct Approach

To solve this properly, your implementation must:

1. **Group all index entries by directory**
2. **Build the tree hierarchy bottom-up**, starting from the deepest directories
3. **Generate each tree object only once**, when all its children (blobs or subtrees) are known

This ensures:

* Zero redundant work
* Clean, deterministic tree construction
* Efficient object storage (Git won’t store the same tree multiple times)


### Summary

The `.git/index` acts as a high-performance staging layer. It tells `write-tree` exactly what to snapshot. Without this design, Git would have to scan and hash the entire working directory for every commit, making it slow and inefficient.

Implementing `write-tree` forces you to understand:

* The index file structure
* The way Git represents trees and directories
* How Git avoids redundancy by reusing identical tree objects

This challenge is a fundamental building block toward understanding how Git internally snapshots your project state — and ultimately how version control at scale works under the hood.


## Challenge 06 - Create a commit
```bash
git commit-tree
```

In this stage, we implement a simplified version of the `git commit` command, which is responsible for creating commit objects in Git. A commit object is the heart of Git's version control system, encapsulating the state of the project at a particular point in time, along with critical metadata.

### Purpose

This challenge focuses on understanding how Git tracks project history. Unlike other VCS tools that track differences, Git uses snapshots — each commit represents a complete snapshot of the working directory at a given moment. This implementation aims to deepen the understanding of Git’s object model and the role commit objects play.

### What Does a Commit Object Contain?

A commit object includes the following information:

* The SHA of the root tree object (snapshot of the staged files)
* Author name and email
* Committer name and email (we treat these the same here)
* Timestamps (Unix epoch + timezone offset)
* A commit message
* The SHA(s) of the parent commit(s), if any

### How `git commit` Works Internally

When you run `git commit`, Git performs several low-level steps:

1. Calls `git write-tree` to serialize the index into a tree object and get its SHA
2. Creates a commit object using `git commit-tree`, embedding:

   * The tree SHA from step 1
   * Author and committer info
   * The parent commit (if any)
   * The commit message
3. Stores the commit object under `.git/objects/`
4. Updates the reference (e.g., `refs/heads/main`) to point to this new commit

### Simplifications in This Challenge

To keep things focused:

* We assume `git write-tree` has already been called manually, and its output (the tree SHA) is passed directly as an argument
* The parent commit SHA is also passed manually
* We’re working with a linear history only (no branching or merging), so only a single parent SHA is allowed

### Commit Object Format

Git objects are stored in the format:

```
<type> <size>\0<content>
```

In this case, the type is `commit`. The content of a commit object looks like this:

```
tree <tree_sha>
parent <parent_sha>
author <name> <email> <timestamp> <timezone>
committer <name> <email> <timestamp> <timezone>

<commit message>
```

Each field must be encoded precisely to match Git’s internal expectations. Even a missing newline or space can corrupt the repository.

### Summary

This stage cements the idea that Git is a content-addressed storage system. Every commit object is uniquely identified by the SHA-1 hash of its content. By implementing `commit-tree`, we gain insight into how history is recorded, and how commits form a linked list of snapshots. This low-level operation is normally hidden behind Git’s porcelain commands, but understanding it is key to mastering Git internals.

With this implemented, we now have the ability to create new commits and begin forming a real Git history — one commit at a time.


## Challenge 07 - Clone a Repository

This challenge focuses on implementing the core functionality of the `git clone` command using Git's **Smart HTTP** protocol and internal object management.

### Overview

When cloning a repository using `git clone`, the Git client must first fetch metadata from the remote server to understand the state of the repository. This includes branch references, HEAD position, and the list of Git objects (commits, trees, blobs) required to reconstruct the full history.

To accomplish this, Git communicates over the **Smart HTTP protocol** using a series of well-defined steps:


### Smart HTTP Protocol

1. **GET /info/refs?service=git-upload-pack**:

   * Returns a [pkt-line](https://git-scm.com/docs/pack-protocol/2.21.0) formatted list of references and capabilities.
   * First line is a header: e.g., `001e# service=git-upload-pack\n0000`
   * Second line: hash, ref, and after `\0` — capabilities (e.g., multi\_ack, thin-pack, symref=HEAD\:refs/heads/master)

2. **Extract HEAD Reference**:

   * Parse the `refs` response to get the current HEAD position, branches, and capabilities.

3. **Send git-upload-pack Request (want-have)**:
   * In this step, we send a request to the server (e.g., GitHub) specifying which Git objects we want and which ones we already have.
  
      Since we're performing a clone, we don't have any objects yet — we're starting from scratch.  

   * Construct the request:

     ```
     0032want <HEAD-hash>\n
     00000009done\n
     ```
      - The `want` line specifies which object we want — in this case, the SHA-1 hash of the **HEAD reference**, which typically points to the latest commit on the default branch (like `main`).
      - The `done` line signals that we've finished listing what we want and have.
   * Content-Type header must be: `application/x-git-upload-pack-request`
   * Although we're not including any have lines in this example (because we don't have any objects yet), the same want-have protocol is also used for git fetch and git pull.
  
      In those cases, the client sends a list of have hashes to inform the server which objects it already possesses, allowing the server to avoid sending duplicates — improving efficiency.

4. **Server Response**:

   * Starts with `0008NAK`, indicating no common commits
   * Followed by the `.pack` file (binary stream)



### Parsing the .pack File

The pack file contains all objects (commits, trees, blobs, deltas) in a compressed form.

### Pack File Structure

1. **Header**:

   * First 4 bytes: `PACK`
   * Next 4 bytes: version (e.g., 00000002)
   * Next 4 bytes: number of objects  (Big-Endian)

2. **Object Entries**:

   * Each object starts with a header byte:

     * Type (always bits 6-4) 
       - COMMIT = 1
       - TREE = 2
       - BLOB = 3
       - TAG = 4
       - OFS_DELTA = 6
       - REF_DELTA = 7 [More Here](./refs/RefDelta)

     * Size of uncompressed object (bits 3-0 + bits 6-0 from next byte if MSB==1)
     * Bit 7: MSB flag (if 1, continue reading size from next byte)

### Steps


### 1. Read the Pack File Header

The first 12 bytes of a `.pack` file form the header and contain:

* A 4-byte magic signature: `"PACK"`
* A 4-byte version number (usually 2 or 3)
* A 4-byte big-endian unsigned integer indicating the number of objects contained in the pack

This tells us how many objects we need to parse from the file.

---

### 2. Read and Parse Objects

After the header, the file consists of back-to-back **zlib-compressed Git objects**. Each object has:

* A **custom Git header** (not zlib!) that encodes:

  * The type of the object (commit, tree, blob, tag, or delta)
  * The size of the uncompressed object (in a variable-length format)
* For **REF\_DELTA** objects, there is an additional 20-byte SHA-1 hash after the type/size header. This hash points to the **base object** the delta references.
* Finally, the rest of the object is zlib-compressed data.

---

### 3. Handle Object Boundaries

One of the tricky parts of packfile parsing is determining where one object ends and the next begins. Since Git compresses each object individually using zlib, there's no explicit delimiter between objects.

However, there's a key observation:

> Zlib-compressed data has a well-defined header, and zlib decompressors are capable of recognizing the end of a compressed stream.

So, once we know where a zlib stream starts, we can decompress it fully and stop right at the start of the next object.

👉 **Note**: It's important to use a streaming decompressor that can stop at the end of a compressed block (e.g., Go's `zlib.NewReader` does this correctly).

---

### 4. Extract All Objects

Using the method above, we can read all objects in the `.pack` file, decompress them, and parse their headers and content. This gives us all the raw data we need to reconstruct the Git repository.

---

### 5. Write Git Objects to .git/objects

Once all objects are parsed:

* For each object, calculate its SHA-1 hash (based on its **uncompressed** format: `<type> <size>\0<content>`)
* Store the object in `.git/objects/` using Git's object storage format:

  * First two hex digits of the hash become a folder
  * Remaining 38 hex digits become the filename inside that folder
  * Content is stored in zlib-compressed form

---

### 6. Reconstruct Working Directory (Render Files)

Now that we have all the objects locally, we can rebuild the full working directory:

* Start from the commit object pointed to by `HEAD`
* Recursively walk the tree object referenced by the commit
* For each tree entry:

  * If it's a **blob**, decompress and write the file
  * If it's a **subtree**, create the directory and recurse into it

This is best done using **DFS (depth-first search)** to ensure we fully populate nested directories before moving up.




### Endianness

Git uses **Big-Endian (network byte order)** in the pack file, whereas most modern CPUs use **Little-Endian**. Special care must be taken when interpreting binary data to avoid misreading multi-byte integers.


### Conclusion

This stage involved reverse-engineering Git's Smart HTTP protocol, parsing references, handling `.pack` files, and learning how Git saves space using techniques like var-length integers, Big-Endian encoding, and delta compression with `REF_DELTA`. The `.pack` file is so tightly packed that unpacking it required carefully walking byte-by-byte and decompressing each object in sequence. This was one of the most technically challenging stages, but also the most insightful — offering a glimpse into the raw efficiency of Git’s internals.

