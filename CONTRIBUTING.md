# Conventions

## Language

- **English only** in code, comments, documents, commit messages, identifiers, and file names.
- **Plain words.** Describe what happens. Say "write a checkpoint", not "persist a recovery
  artifact". Say "the controller stopped", not "the governor lost its lease".
- **Define a term once**, where it first appears, and then use it consistently.
- **No internal slang, no metaphors used as names, no marketing adjectives.** Words like "smart",
  "seamless", "powerful", "robust" are removed on sight.
- **Short sentences, one idea each.** If a sentence needs a comma to carry two ideas, it is two
  sentences.
- **Comments explain why.** The code already says what. A comment that repeats the code is deleted.

## Naming

- Packages are named for what they do: `policy`, `observe`, `advise`.
- Files are named for their content. `helpers.go`, `utils.go`, `common.go` are not allowed.
- The only abbreviation allowed is `ckpt` in binary and directory names. Documents spell
  "checkpoint".
- A name that needs a comment to be understood is the wrong name.

## Numbers

- A number in a document comes from a test, a trace, or a measurement, and names its source.
- A number that is a target rather than a measurement is labelled as a target.
- A missing measurement is written as unknown. It is never written as zero.

## Tests

- The policy is tested with properties over generated traces, not only with examples.
- Comparisons are made by replaying the same trace through several policies.
- Any test that needs a cluster is separate from the unit tests and runs once per change.

## Commits

- English, one milestone per commit.
- The message states what changed and the number that proves it.
- Never commit generated output that a command can reproduce.
