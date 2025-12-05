# X Agent Architecture Deep Dive

## System Overview

The `x-agent` is a modular CLI application designed to automate interactions with the X (Twitter) API. It uses a layered architecture to separate concerns:

1.  **CLI Layer (`cli.py`)**: Entry point. Parses arguments, configures logging, and instantiates the appropriate Agent.
2.  **Agent Layer (`agents/`)**: Implements business logic for specific tasks (e.g., `UnblockAgent`, `InsightsAgent`). Agents are resumable and state-aware.
3.  **Service Layer (`services/x_service.py`)**: Encapsulates API complexity. Handles authentication, rate limiting, pagination, and error handling. Abstractions over `tweepy`.
4.  **Persistence Layer (`database.py`)**: SQLite database for tracking state (e.g., which IDs have been processed), ensuring resumability.

## Architecture Diagram (Mermaid)

```mermaid
graph TD
    subgraph Local Environment
        User[User / CLI] -->|Run Command| CLI[cli.py]
        CLI -->|Instantiate| Service[XService]
        CLI -->|Instantiate| Agent[UnblockAgent]
        
        Agent -->|Use| Service
        Agent -->|Read/Write| DB[(SQLite Database)]
        
        subgraph "XService Logic"
            Service -->|Auth (OAuth 1.0a)| Auth[Authentication]
            Service -->|Rate Limit| RL[Rate Limit Handler]
            Service -->|Fetch| GetBlocked[get_blocked_user_ids]
            Service -->|Action| Unblock[unblock_user]
        end
    end

    subgraph "X (Twitter) API"
        GetBlocked -->|GET /2/users/:id/blocking| API_Get[API V2: Get Blocked]
        Unblock -->|DELETE /2/users/:id/blocking/:target_id| API_Del[API V2: Unblock]
    end

    API_Get -->|Return User Objects| GetBlocked
    API_Del -->|Return 200 or 404| Unblock
```

## Current Issue Deep Dive: The "Ghost Block" Paradox

The current issue involves a discrepancy where the API returns a user as "Blocked" but refuses to "Unblock" them.

### Flow Trace
1.  **Fetch**: `UnblockAgent` calls `XService.get_blocked_user_ids()`.
2.  **API Call**: `XService` uses `tweepy.Paginator(client_v2.get_blocked)`.
    *   **Result**: The API returns a list of `User` objects (e.g., ID `1407714921828306951`).
    *   *Implication*: These users definitely exist and are blocked according to the V2 Read endpoint.
3.  **Unblock**: `UnblockAgent` iterates and calls `XService.unblock_user(1407714921828306951)`.
4.  **API Call**: `XService` executes `DELETE /2/users/{me}/blocking/1407714921828306951`.
5.  **Result**: `404 Not Found`.

### Potential Causes

1.  **Endpoint Specificity**: The `DELETE` endpoint might enforce stricter validation than `GET`. If a user is suspended, `GET` might list them (legacy artifact), but `DELETE` might fail to find the active user object to unblock.
2.  **ID Mismatch**: While unlikely with integers, if there is any string/int conversion issue in the library, it could cause this. (Verified as unlikely).
3.  **Authentication Context**: The `DELETE` endpoint acts on behalf of the *authenticated user*. If the `client_v2` was somehow authenticated as a different user (App-only context vs User context), it might fail. However, logs show "Authentication successful".

### Next Steps
We have added detailed logging to capture the **Response Body** of the 404 error. This will reveal the specific API error code (e.g., `User Not Found` vs `Resource Not Found`) to pinpoint the root cause.
