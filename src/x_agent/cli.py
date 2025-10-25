import sys
import logging
import argparse
from .services.x_service import XService
from .agents.unblock_agent import UnblockAgent
from .logging_setup import setup_logging


def main() -> None:
    """
    Main entry point for the CLI.

    Parses command-line arguments, initializes the XService, and runs the
    specified agent.
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
