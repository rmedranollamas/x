"""Tests for the logging_setup module."""
import logging
import io
import sys
import pytest
from unittest.mock import patch, MagicMock
from x_agent.logging_setup import SingleLineUpdateHandler, setup_logging


class TestSingleLineUpdateHandler:
    """Tests for SingleLineUpdateHandler class."""

    def test_init_defaults(self):
        """Test that handler initializes with default values."""
        handler = SingleLineUpdateHandler()
        assert handler._last_single_line_length == 0

    def test_emit_regular_message(self, capsys):
        """Test emitting a regular (non-single-line) message."""
        stream = io.StringIO()
        handler = SingleLineUpdateHandler(stream)
        
        record = logging.LogRecord(
            name="test",
            level=logging.INFO,
            pathname="test.py",
            lineno=1,
            msg="Test message",
            args=(),
            exc_info=None,
        )
        
        handler.emit(record)
        output = stream.getvalue()
        assert "Test message" in output

    def test_emit_single_line_message(self, capsys):
        """Test emitting a single-line message."""
        stream = io.StringIO()
        # Mock isatty to return True
        stream.isatty = MagicMock(return_value=True)
        handler = SingleLineUpdateHandler(stream)
        
        record = logging.LogRecord(
            name="test",
            level=logging.INFO,
            pathname="test.py",
            lineno=1,
            msg="Processing...",
            args=(),
            exc_info=None,
        )
        record.single_line = True
        
        handler.emit(record)
        output = stream.getvalue()
        # Single line messages should use \r
        assert "\rProcessing..." in output

    def test_emit_single_line_overwrites_previous(self):
        """Test that single-line messages overwrite previous ones."""
        stream = io.StringIO()
        stream.isatty = MagicMock(return_value=True)
        handler = SingleLineUpdateHandler(stream)
        
        # First message
        record1 = logging.LogRecord(
            name="test",
            level=logging.INFO,
            pathname="test.py",
            lineno=1,
            msg="Short",
            args=(),
            exc_info=None,
        )
        record1.single_line = True
        handler.emit(record1)
        
        # Second, longer message
        record2 = logging.LogRecord(
            name="test",
            level=logging.INFO,
            pathname="test.py",
            lineno=1,
            msg="This is a much longer message",
            args=(),
            exc_info=None,
        )
        record2.single_line = True
        handler.emit(record2)
        
        output = stream.getvalue()
        # Should have cleared the previous line
        assert "\r" in output
        assert "This is a much longer message" in output

    def test_emit_closed_stream(self):
        """Test that emit handles closed streams gracefully."""
        stream = io.StringIO()
        stream.close()
        handler = SingleLineUpdateHandler(stream)
        
        record = logging.LogRecord(
            name="test",
            level=logging.INFO,
            pathname="test.py",
            lineno=1,
            msg="Test",
            args=(),
            exc_info=None,
        )
        
        # Should not raise an exception
        handler.emit(record)

    def test_emit_non_tty_stream(self):
        """Test that single_line messages work on non-TTY streams."""
        stream = io.StringIO()
        stream.isatty = MagicMock(return_value=False)
        handler = SingleLineUpdateHandler(stream)
        
        record = logging.LogRecord(
            name="test",
            level=logging.INFO,
            pathname="test.py",
            lineno=1,
            msg="Test message",
            args=(),
            exc_info=None,
        )
        record.single_line = True
        
        handler.emit(record)
        output = stream.getvalue()
        # On non-TTY, should just print normally
        assert "Test message" in output

    def test_emit_transitions_from_single_to_regular(self):
        """Test transition from single-line to regular message."""
        stream = io.StringIO()
        stream.isatty = MagicMock(return_value=True)
        handler = SingleLineUpdateHandler(stream)
        
        # Single line message
        record1 = logging.LogRecord(
            name="test",
            level=logging.INFO,
            pathname="test.py",
            lineno=1,
            msg="Loading...",
            args=(),
            exc_info=None,
        )
        record1.single_line = True
        handler.emit(record1)
        
        # Regular message
        record2 = logging.LogRecord(
            name="test",
            level=logging.INFO,
            pathname="test.py",
            lineno=1,
            msg="Done!",
            args=(),
            exc_info=None,
        )
        handler.emit(record2)
        
        output = stream.getvalue()
        # Should have cleared the line and printed on new line
        assert "\r" in output
        assert "Done!" in output

    def test_emit_with_formatter(self):
        """Test that formatter is applied to messages."""
        stream = io.StringIO()
        handler = SingleLineUpdateHandler(stream)
        
        formatter = logging.Formatter("%(levelname)s: %(message)s")
        handler.setFormatter(formatter)
        
        record = logging.LogRecord(
            name="test",
            level=logging.INFO,
            pathname="test.py",
            lineno=1,
            msg="Test message",
            args=(),
            exc_info=None,
        )
        
        handler.emit(record)
        output = stream.getvalue()
        assert "INFO: Test message" in output


class TestSetupLogging:
    """Tests for setup_logging function."""

    def test_setup_logging_default(self):
        """Test setup_logging with default parameters."""
        # Clear existing handlers
        logger = logging.getLogger()
        original_handlers = logger.handlers.copy()
        
        try:
            setup_logging(debug=False)
            
            # Check that a handler was added
            assert len(logger.handlers) > 0
            
            # Check that the handler is SingleLineUpdateHandler
            assert any(isinstance(h, SingleLineUpdateHandler) for h in logger.handlers)
            
            # Check log level is INFO
            assert logger.level == logging.INFO
        finally:
            # Restore original handlers
            for handler in logger.handlers[:]:
                logger.removeHandler(handler)
            for handler in original_handlers:
                logger.addHandler(handler)

    def test_setup_logging_debug(self):
        """Test setup_logging with debug=True."""
        logger = logging.getLogger()
        original_handlers = logger.handlers.copy()
        original_level = logger.level
        
        try:
            setup_logging(debug=True)
            
            # Check log level is DEBUG
            assert logger.level == logging.DEBUG
            
            # Check that a handler was added
            assert any(isinstance(h, SingleLineUpdateHandler) for h in logger.handlers)
        finally:
            # Restore original state
            for handler in logger.handlers[:]:
                logger.removeHandler(handler)
            for handler in original_handlers:
                logger.addHandler(handler)
            logger.setLevel(original_level)

    def test_setup_logging_removes_existing_handlers(self):
        """Test that setup_logging removes existing handlers."""
        logger = logging.getLogger()
        
        # Add a dummy handler
        dummy_handler = logging.StreamHandler()
        logger.addHandler(dummy_handler)
        
        assert dummy_handler in logger.handlers
        
        setup_logging(debug=False)
        
        # Dummy handler should be removed
        assert dummy_handler not in logger.handlers
        
        # Clean up
        for handler in logger.handlers[:]:
            logger.removeHandler(handler)

    def test_setup_logging_formatter(self):
        """Test that setup_logging uses the correct formatter."""
        logger = logging.getLogger()
        original_handlers = logger.handlers.copy()
        
        try:
            setup_logging(debug=False)
            
            # Get the SingleLineUpdateHandler
            handler = next(
                (h for h in logger.handlers if isinstance(h, SingleLineUpdateHandler)),
                None
            )
            
            assert handler is not None
            assert handler.formatter is not None
            
            # Check formatter format
            record = logging.LogRecord(
                name="test",
                level=logging.INFO,
                pathname="test.py",
                lineno=1,
                msg="Test",
                args=(),
                exc_info=None,
            )
            formatted = handler.format(record)
            
            # Should contain timestamp, level, and message
            assert "INFO" in formatted
            assert "Test" in formatted
        finally:
            for handler in logger.handlers[:]:
                logger.removeHandler(handler)
            for handler in original_handlers:
                logger.addHandler(handler)
