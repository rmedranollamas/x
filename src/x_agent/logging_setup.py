import sys
import logging


class SingleLineUpdateHandler(logging.StreamHandler):
    """
    A logging handler that updates a single line in the console.

    This handler uses a carriage return (`\r`) to move the cursor to the
    beginning of the line, allowing subsequent log messages to overwrite
    the previous one. This is useful for displaying real-time progress
    updates without cluttering the console.
    """

    def __init__(self, *args, **kwargs):
        super().__init__(*args, **kwargs)
        self._last_single_line_length = 0

    def emit(self, record: logging.LogRecord) -> None:
        """
        Emits a log record, overwriting the previous line if necessary.

        Args:
            record: The log record to emit.
        """
        message = self.format(record)
        if hasattr(record, "single_line"):
            # Clear the previous line if necessary
            if self._last_single_line_length > len(message):
                print(
                    " " * self._last_single_line_length,
                    end="\r",
                    file=sys.stdout,
                    flush=True,
                )
            print(f"\r{message}", end="", file=sys.stdout, flush=True)
            self._last_single_line_length = len(message)
        else:
            # If a non-single-line record comes, clear any active single-line message
            if self._last_single_line_length > 0:
                print(
                    " " * self._last_single_line_length,
                    end="\r",
                    file=sys.stdout,
                    flush=True,
                )
                self._last_single_line_length = 0
            print(message, file=self.stream, flush=True)


def setup_logging(debug: bool = False) -> None:
    """
    Configures the root logger for the application.

    This function sets up a logger with a custom handler that supports
    single-line updates. It removes any existing handlers to prevent
    duplicate logging.

    Args:
        debug: If True, sets the logging level to DEBUG.
    """
    log_level = logging.DEBUG if debug else logging.INFO
    logger = logging.getLogger()
    logger.setLevel(log_level)

    # Remove any existing handlers to avoid duplicate logs
    for handler in logger.handlers[:]:
        logger.removeHandler(handler)

    handler = SingleLineUpdateHandler(sys.stdout)
    formatter = logging.Formatter(
        "%(asctime)s - %(levelname)s - %(message)s", datefmt="%Y-%m-%d %H:%M:%S"
    )
    handler.setFormatter(formatter)
    logger.addHandler(handler)
