### `REF_DELTA` Git Object

Git internally uses various object types to store information efficiently — blobs, trees, commits, and *delta objects*. One of the most space-saving features in Git's object storage is the use of `REF_DELTA` and `OFS_DELTA` objects inside `.pack` files. These are delta-compressed representations of other objects.

This document focuses on the `REF_DELTA` type.

---

#### What is a `REF_DELTA`?

A `REF_DELTA` is a type of object in a Git `.pack` file that is not a full object by itself. Instead, it stores a *delta* — the difference between an existing object and a new version — and a reference to the original object (called the *base object*).

It consists of:

* A header byte indicating the object type (bits 4–6 will be `6`, which is the type code for REF\_DELTA).
* A SHA-1 hash (20 bytes) of the base object that this delta applies to.
* The actual delta content — a compressed binary patch that describes how to transform the base object into the new object.

---

#### Motivation: Why Use Delta Compression?

Imagine you have a `README.md` file that's 1000 bytes long, and you make a minor change — like adding a single line. Without deltas, Git would have to store the entire updated file again as a new blob (1010 bytes).

With `REF_DELTA`, Git instead does something smarter:

> "Copy the 1000 bytes from the existing object, then add these 10 new bytes at the end."

This saves space — especially in large repositories or projects with frequent, small edits.

---

#### Example Use Case

Let’s say your repo has a file `config.yml`, and over time you make 20 small edits to it. If Git stored a full blob for every version, your `.git/objects` directory would grow quickly.

Using `REF_DELTA`, Git can store:

* One full version of the object (the base).
* 19 small delta objects that each describe how to get from the base to the next version.

When Git needs to reconstruct a version, it applies the chain of deltas in reverse until it reaches the full base object, then applies each patch in order.

---

#### How to Handle `REF_DELTA` in Your Git Client Implementation

When reading a `.pack` file and encountering a `REF_DELTA`:

1. Read the object header.
2. Extract the 20-byte SHA-1 of the base object.
3. Parse and decompress the delta content (usually via zlib).
4. Resolve the base object — it may be in the pack or already stored in `.git/objects/`.
5. Apply the delta algorithm to reconstruct the full object.

Note: The delta format has a specific binary encoding — a sequence of copy/insertion commands that describe how to build the new object.

#### Summary

`REF_DELTA` objects are a crucial optimization in Git’s object storage model. They reduce redundancy and storage space, particularly in large repositories with many similar versions of files.

If you're writing a Git client or implementing packfile parsing, handling `REF_DELTA` correctly is essential to ensure that you can reconstruct all objects accurately.

Always remember:

* They are not full objects.
* They depend on the base object's availability.
* They must be patched to obtain the final, usable object content.
