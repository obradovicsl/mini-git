# 🔍 Git Object Types & Their Internal Structure

Git internally stores **four types of objects**, all of which share a common binary layout, but differ in what their `content` represents. These objects are fundamental to Git’s versioning mechanism. Name of an object is its hash (e88f7a929cd70b0274c4ea33b209c97fa845fdbc).

---

## ✅ Object Types

### 1. **Blob**

* **Stores:** File content (but **not** filename or metadata)
* **Purpose:** Represents the contents of a file in the working tree.

### 2. **Tree**

* **Stores:** Directory structure (file names, modes, and object references)
* **Purpose:** Represents a snapshot of a directory; can reference blobs and other trees.

### 3. **Commit**

* **Stores:** Metadata about a snapshot
* **Includes:** Reference to a tree, author, committer, commit message, parent commits
* **Purpose:** Represents a single revision in Git history.

### 4. **Tag**

* **Stores:** Metadata for annotated tags
* **Includes:** Reference to another object (usually a commit), tagger info, tag message
* **Purpose:** Provides a human-readable reference to specific commits

---

## 📁 Where Are These Objects Stored?

All loose Git objects are stored inside the `.git/objects/` directory. For example:

```
.git/objects/ab/cdef1234...
```

* The first 2 characters of the SHA-1 become the directory name (`ab`), the rest become the filename (`cdef1234...`)
* Over time, Git may compress and store multiple objects inside a **packfile**, located in `.git/objects/pack/`

---

## 🧬 Object Format in Storage

All Git objects are stored in the following format:

```
<object_type> <content_length>\0<content>
```

* `<object_type>` is one of: `blob`, `tree`, `commit`, or `tag`
* A **null byte (`\0`)** separates the header from the content
* The entire object (header + content) is **zlib-compressed** when stored in `.git/objects/`

---

## 📦 Content Breakdown per Object

### 📄 Blob

```text
Content = raw file data
```

* No headers, metadata, or structure
* Example: For a file `hello.txt` containing `Hello\n`, the blob content is just `Hello\n`

---

### 🌲 Tree

```text
Content = multiple entries, each like:
<mode> <type> <20-byte object id> <filename>
```

* `mode`: File mode in ASCII (e.g. `100644` for a regular file, `100755` for executable file, `40000` for a tree)
* `filename`: Name of the file or directory
* `object id`: 20-byte **binary** SHA-1 hash (not hex) of the object being referenced
* Entries are **concatenated** without newlines

**Example entry:**

```
100644 blob <blob_sha_1> hello.txt
040000 tree <tree_sha> directory1
```

---

### 📝 Commit

```text
Content = ASCII text:
tree <SHA-1 of root tree>
parent <SHA-1 of parent commit>        # optional
author <name> <email> <timestamp> <timezone>
committer <name> <email> <timestamp> <timezone>

<empty line>
<commit message>
```

* `parent` lines are optional and can appear multiple times (for merge commits)
* Timestamps are in **Unix epoch** format

---

### 🏷️ Tag

```text
Content = ASCII text:
object <SHA-1 of referenced object>
type <object type>                     # e.g. commit
tag <tag name>
tagger <name> <email> <timestamp> <timezone>

<empty line>
<tag message>
```

---

## 🔑 Notes

* Git computes SHA-1 hash over the **uncompressed** full data: `<type> <size>\0<content>`
* The object content is then **zlib-compressed** before being stored
* Packed objects may be stored using **deltas** and require special handling (see `REF_DELTA`, `OFS_DELTA`)

---

Feel free to extend this file with implementation notes or parsing tips if you're building your own Git tooling!
