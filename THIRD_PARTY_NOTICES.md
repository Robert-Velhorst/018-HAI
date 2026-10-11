# Third-Party Notices

## Redpanda Community Edition

HAI's local Compose topology uses the unmodified Redpanda Community Edition
container as a Kafka-compatible event broker. It is not linked into HAI and is
not exposed as a streaming or queuing service.

- Project: https://github.com/redpanda-data/redpanda
- Copyright: Redpanda Data, Inc.
- License: Redpanda Business Source License 1.1
- License text: https://github.com/redpanda-data/redpanda/blob/dev/licenses/bsl.md
- Pinned release: v26.2.1
- Change license: Apache License 2.0 on the release-specific change date

The Additional Use Grant does not permit offering Redpanda as a commercial
streaming or queuing service. Review the linked license before changing HAI's
distribution or service model.

## BooBoo

HAI's Operational Brain uses concepts adapted from the BooBoo graph specification and organization boot-slice design. The integration is a native Go/PostgreSQL/Angular implementation and does not ship BooBoo's Node.js server, React viewer, journal, or container.

- Project: [booboo](https://github.com/jessymariau/booboo)
- Copyright: Copyright (c) 2026 Jessy Mariau
- License: MIT
- Source reviewed: user-supplied `booboo-main.zip`

```text
MIT License

Copyright (c) 2026 Jessy Mariau

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```
