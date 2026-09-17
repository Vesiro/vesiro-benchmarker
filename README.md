# vesiro-benchmarker

`vesiro-benchmarker` is a command-line benchmarking tool for Elasticsearch.

## Build

Building requires Go 1.23 or newer. Run these commands from the repository
root:

```sh
make build
./bin/bench --help
```

This builds the program at `bin/bench`. You can also build without `make` using
`go build -o bin/bench ./cmd/bench`.

## Run your first benchmark

You'll need a running Elasticsearch node with data already in an index.

Start with the included [match_all.json](assets/query/templates/match_all.json).
It works with any index because it doesn't depend on specific fields. It matches
all documents and returns a page of hit details.

Send one request to check the connection. Replace `my-index` with your index name
and `http://localhost:9200` with your node's address:

```sh
./bin/bench single \
  --node-url=http://localhost:9200 \
  --index-name=my-index \
  --query-template=assets/query/templates/match_all.json
```

`--query-template` is the path to a query template file. `single` prints the
query, HTTP status, response time, and response body. If your node needs a
login, add `--user='username:password'` and use an `https://` URL for HTTPS,
or keep them in a config file (see [Saved defaults](#saved-defaults)).

Once that works, send 100 requests from each of four clients:

```sh
./bin/bench run \
  --node-url=http://localhost:9200 \
  --index-name=my-index \
  --query-template=assets/query/templates/match_all.json \
  --num-clients=4 \
  --requests-per-client=100
```

A client sends one request, waits for the response, then sends the next. Four
clients means up to four requests can be in progress at once, all from a
single `bench` process. This run sends 400 search requests in total.

## Read the results

Here's an example report for a run with 400 requests:

```text
Requests:        400
Ok:              400
Errors:          0 (0.00%)
Statuses:        200: 400
q/s:             250.00
Execution Time:  1.6s
Took Avg:        12.00ms
Client Latency:  avg 15.00ms  p50 14.00ms  p95 24.00ms  p99 30.00ms
HasHits:         400 (100.0%)
Per Query:
  match_all.json: 400 requests  avg 15.00ms  p50 14.00ms  p95 24.00ms  p99 30.00ms
```

| Field | What it tells you |
| --- | --- |
| `Requests` | How many responses were included in the report. |
| `Ok` / `Errors` | How many search responses succeeded or failed. |
| `Statuses` | How often each HTTP status occurred. Here, all 400 responses were HTTP 200. |
| `q/s` | Requests per second over the whole run. |
| `Execution Time` | How long the run took. |
| `Took Avg` | Average search time reported by the node. |
| `Client Latency` | Time measured by `bench`, from sending a request to reading its full response. |
| `HasHits` | Successful searches where the node reported at least one matching document. |
| `Per Query` | Request count and response times for each query. |

Client latency includes the network trip. The node's `took` covers its own work,
so the two measure different things.

Check errors before comparing timings. Failed search responses are included in
the timings. Connection failures, request timeouts, or invalid response JSON
stop the run; those requests aren't counted in the report.

To save the report as JSON, add `--output=json > report.json` to your benchmark
command.

## Common Crawl queries

The [cc-wet folder](assets/query/templates/cc-wet/) contains 80 query templates
for Common Crawl WET data, grouped by query type.

These templates need an index with the mappings defined in
[cc-wet.json](assets/query/mappings/cc-wet.json). You can use
[vesiro-indexer](https://github.com/Vesiro/vesiro-indexer) to set up an index
with Common Crawl data.

## Change the load

| Option | What it does |
| --- | --- |
| `--num-clients=4` | Send requests from four clients at once. The default is 16. |
| `--requests-per-client=100` | Send 100 requests per client, multiplied by `--repeat-each-request`. |
| `--benchmark-timeout=60` | Stop the run after 60 seconds. |
| `--qps=200` | Limit the average rate to 200 requests per second across all clients. The actual rate may be lower. |
| `--request-timeout=10` | Stop the run if a request takes longer than 10 seconds. |

Set a request count, a run duration, or both. With both set, the run stops at
whichever limit it reaches first. You can also stop a run with Ctrl-C and get a
report of the results collected so far.

Without `--qps`, each client sends its next request as soon as the previous one
finishes. To limit the average rate to 200 requests per second for one minute, run:

```sh
./bin/bench run \
  --node-url=http://localhost:9200 \
  --index-name=my-index \
  --query-template=assets/query/templates/match_all.json \
  --num-clients=16 \
  --benchmark-timeout=60 \
  --qps=200
```

## Choose your queries

The four commands use the same query template files:

| Command | When to use it |
| --- | --- |
| `single` | Send one request using a query template file and read the response. |
| `run` | Benchmark one query template file. |
| `folder` | Benchmark a mix of query template files. |
| `render` | Preview the JSON from a query template file without sending a request. |

### Define a query template

A template lets you change parts of the query, such as the search terms. The
query goes inside a `template` field, and `bindings` supplies the values to fill
in before each request.

For example, [match_or.json](assets/query/templates/cc-wet/full-text/match_or.json)
searches the `content` field with phrases from a text file. Its main fields are:

```json
{
  "name": "match_or",
  "bindings": {
    "Phrase": {
      "source": "file",
      "path": "./assets/query/terms/phrase-2.txt"
    }
  },
  "template": {
    "_source": false,
    "query": {
      "match": { "content": { "query": "{{.Phrase}}" } }
    }
  }
}
```

File paths inside templates are relative to the directory where you run `bench`.
Run the bundled examples from the repository root so their term files can be found.

### Run a mix of queries

`folder` randomly picks a query file for each new request. You can use your own
folder or one of the included query folders.

This example sends 1,600 requests in total, spread across the files in the
folder:

```sh
./bin/bench folder \
  --node-url=http://localhost:9200 \
  --index-name=my-index \
  --query-folder=assets/query/templates/cc-wet/boolean \
  --num-clients=16 \
  --requests-per-client=100
```

The request count applies to the whole run, not to each query file. The report
shows how many times each query ran.

The [templates folder](assets/query/templates/) also includes full-text, range,
and other queries.

## Saved defaults

For settings you use often, copy [config.example.json](config.example.json) to
`config.json`. Anything set there no longer needs to be passed as a
command-line flag.

## Best practises

- Keep warm-up in mind when comparing results. Early requests can be slower
  while the node's caches and runtime warm up.
- Keep the data, queries, client count, and background traffic consistent.
- Run `bench` on a separate machine when possible, so it
  doesn't compete with the search node for CPU and memory.

## License

[Apache License 2.0](LICENSE).
