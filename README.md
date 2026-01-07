# Grain

A simple, scalable, and real-time chat application built with Go, WebSockets, and Redis.

## Architecture

```mermaid
graph TD
    subgraph Clients
        A[Client 1]
        B[Client 2]
        C[Client 3]
    end

    subgraph Servers
        S1[Go App 1]
        S2[Go App 2]
    end

    subgraph Database
        R[Redis Instance]
        subgraph Redis Features
            PS[Pub/Sub]
            H[History]
        end
    end

    A -- "connects to" --> S1
    B -- "connects to" --> S2
    C -- "connects to" --> S1

    S1 -- "publishes message" --> PS
    S2 -- "publishes message" --> PS

    PS -- "delivers message" --> S1
    PS -- "delivers message" --> S2

    S1 -- "sends message" --> A
    S1 -- "sends message" --> C
    S2 -- "sends message" --> B
    
    S1 -- "sends historical messages" --> A
    S1 -- "sends historical messages" --> C
    S2 -- "sends historical messages" --> B

    S1 -- "stores message" --> H
    S2 -- "stores message" --> H
```

## Features

- Real-time messaging with WebSockets.
- Scalable architecture using Redis Pub/Sub.
- Chat history persistence.
- Simple and easy-to-use.

## Getting Started

### Prerequisites

- [Go](https://golang.org/dl/)
- [Redis](https://redis.io/download)

### Installation

1.  Clone the repository:

    ```bash
    git clone https://github.com/your-username/grain.git
    ```

2.  Install dependencies:

    ```bash
    go mod tidy
    ```

3.  Run the application:

    ```bash
    go run main.go
    ```

## Usage

Connect to the WebSocket server at `ws://localhost:8080/ws?username=your-username`.

You can use a WebSocket client like [wscat](https://github.com/websockets/wscat) to connect:

```bash
wscat -c "ws://localhost:8080/ws?username=your-username"
```

## Contributing

Contributions are welcome! Please feel free to submit a pull request.

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.
