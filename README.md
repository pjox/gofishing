# gofishing

An extremely fast [entity-fishing](https://github.com/kermitt2/entity-fishing) CLI tool.

Go to [releases](https://github.com/pjox/gofishing/releases) and download the version corresponding to your OS and architecture

gofishing can make concurrent requests to an entity-fishing server allowing you to process a large quantity of PDF documents in no time. It can also format the server response so that you have a human readable JSON file (use tag `-p`).

The **query file is mandatory**.

```text
Usage gofishing:
  -in string
        the location of the PDF files (default "in/")
  -maxnb int
        maximum number of concurrent requests (default 10)
  -out string
        the location where the JSON files will be saved (default "out/")
  -p    format the JSON documents
  -q string
        the name of the query file (default "query.json")
  -s string
        the server address (default "http://cloud.science-miner.com/nerd/service/disambiguate")
```
