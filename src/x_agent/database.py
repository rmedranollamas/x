
import sqlite3
import logging
from pathlib import Path

STATE_DIR = Path(".state")
DB_FILE = STATE_DIR / "insights.db"

def get_db_connection():
    """Establishes a connection to the SQLite database."""
    STATE_DIR.mkdir(exist_ok=True)
    conn = sqlite3.connect(DB_FILE)
    conn.row_factory = sqlite3.Row
    return conn

def initialize_database():
    """Initializes the database with the required schema."""
    conn = get_db_connection()
    cursor = conn.cursor()
    cursor.execute("""
        CREATE TABLE IF NOT EXISTS insights (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
            followers INTEGER,
            following INTEGER
        )
    """)
    conn.commit()
    conn.close()
    logging.info("Database initialized successfully.")

def add_insight(followers, following):
    """Adds a new insight record to the database."""
    conn = get_db_connection()
    cursor = conn.cursor()
    cursor.execute(
        "INSERT INTO insights (followers, following) VALUES (?, ?)",
        (followers, following)
    )
    conn.commit()
    conn.close()
    logging.info(f"Added new insight: {followers} followers, {following} following.")

def get_latest_insight():
    """Retrieves the most recent insight from the database."""
    conn = get_db_connection()
    cursor = conn.cursor()
    cursor.execute("SELECT * FROM insights ORDER BY timestamp DESC LIMIT 1")
    insight = cursor.fetchone()
    conn.close()
    return insight
