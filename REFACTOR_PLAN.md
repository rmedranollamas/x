# Refactoring Plan: X-Unblocker to Multi-Agent Framework

This plan tracks the refactoring of the X Unblocker tool into a scalable, multi-agent architecture.

---

### **Phase 1: Project Scaffolding & Restructuring**

- [x] **Create New Directory Structure:**
    - [x] Create `src/` directory.
    - [x] Create `src/x_agent/` package.
    - [x] Create `src/x_agent/agents/` sub-package.
    - [x] Create `src/x_agent/services/` sub-package.
    - [x] Create placeholder `__init__.py` files.
- [x] **Update Project Configuration (`pyproject.toml`):**
    - [x] Modify `pyproject.toml` to recognize the `src` layout.
    - [x] Add a `[project.scripts]` entry for the `x-agent` command.

---

### **Phase 2: Code Migration & Refactoring**

- [x] **Isolate API Logic into `XService`:**
    - [x] Create `XService` class in `src/x_agent/services/x_service.py`.
    - [x] Move all `tweepy` client creation and authentication logic into `XService`.
    - [x] Move rate limit handling into `XService`.
    - [x] Create methods in `XService` for `get_blocked_ids` and `unblock_user`.
- [x] **Create the Agent Framework:**
    - [x] Define abstract `BaseAgent` class in `src/x_agent/agents/base_agent.py`.
- [x] **Refactor Unblocker into `UnblockAgent`:**
    - [x] Create `UnblockAgent` class in `src/x_agent/agents/unblock_agent.py`.
    - [x] Move unblocking logic from `unblocker.py` into `UnblockAgent.execute()`.
    - [x] Adapt the agent to use the new `XService`.
    - [x] Keep state management (`blocked_ids.txt`, `unblocked_ids.txt`) within the agent.

---

### **Phase 3: Create the CLI Entry Point**

- [x] **Develop `cli.py`:**
    - [x] Create the main entry point in `src/x_agent/cli.py`.
    - [x] Implement `argparse` to select the agent to run (initially, just `unblock`).
    - [x] Handle logging setup.
    - [x] Instantiate `XService` and the selected agent, then run it.

---

### **Phase 4: Finalization & Cleanup**

- [x] **Remove Old Files:**
    - [x] Delete `unblocker.py`.
- [x] **Update Documentation:**
    - [x] Update `README.md` with new setup and execution instructions.
- [x] **Code Quality:**
    - [x] Run `ruff check .` and `ruff format .` across the project.
- [x] **Update Tracking File:**
    - [x] Mark all items in this plan as complete.

