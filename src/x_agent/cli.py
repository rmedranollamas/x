import sys
import logging
import argparse
from .services.x_service import XService
from .agents.unblock_agent import UnblockAgent


class SingleLineUpdateHandler(logging.StreamHandler):
    """A logging handler that uses carriage returns to update a single line."""

    def __init__(self, *args, **kwargs):
        super().__init__(*args, **kwargs)
        self._last_single_line_length = 0

    def emit(self, record):
        message = self.format(record)
        if hasattr(record, "single_line"):
            # Clear the previous line if necessary
            if self._last_single_line_length > len(message):
                print(
                    " " * self._last_single_line_length,
                    end="\\r",
                    file=sys.stdout,
                    flush=True,
                )
            print(f"\\r{message}", end="", file=sys.stdout, flush=True)
            self._last_single_line_length = len(message)
        else:
            # If a non-single-line record comes, clear any active single-line message
            if self._last_single_line_length > 0:
                print(
                    " " * self._last_single_line_length,
                    end="\\r",
                    file=sys.stdout,
                    flush=True,
                )
                self._last_single_line_length = 0
            print(message, file=sys.stdout, flush=True)


def setup_logging(debug=False):
    """Configures the root logger."""
    log_level = logging.DEBUG if debug else logging.INFO
    logger = logging.getLogger()
    logger.setLevel(log_level)

    # Remove any existing handlers to avoid duplicate logs
    for handler in logger.handlers[:]:
        logger.removeHandler(handler)

    handler = SingleLineUpdateHandler()
    formatter = logging.Formatter(
        "%(asctime)s - %(levelname)s - %(message)s", datefmt="%Y-%m-%d %H:%M:%S"
    )
    handler.setFormatter(formatter)
    logger.addHandler(handler)


def main():
    """
    Main entry point for the X Agent CLI.
    Parses arguments, sets up services, and runs the specified agent.
    """
    parser = argparse.ArgumentParser(
        description="A command-line tool to manage X interactions with agents."
    )
    parser.add_argument(
        "agent",
        choices=["unblock"],
        help="The agent to run. Currently available: 'unblock'.",
    )
    parser.add_argument(
        "--debug", action="store_true", help="Enable debug logging for detailed output."
    )
    args = parser.parse_args()

    setup_logging(args.debug)

    try:
        x_service = XService()

        AGENTS = {
            "unblock": UnblockAgent,
        }
        agent_class = AGENTS[args.agent]
        agent = agent_class(x_service)
        agent.execute()

    except Exception as e:
        logging.error(f"An unexpected error occurred: {e}", exc_info=True)
        sys.exit(1)


if __name__ == "__main__":
    main()
