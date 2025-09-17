import os
import sys
import time
import tweepy
from dotenv import load_dotenv

def main():
    """
    Main function to run the X unblocking tool.
    """
    print("--- X Unblocker Tool ---")

    # Load environment variables from .env file
    load_dotenv()

    # Get credentials from environment variables
    api_key = os.getenv("X_API_KEY")
    api_key_secret = os.getenv("X_API_KEY_SECRET")
    access_token = os.getenv("X_ACCESS_TOKEN")
    access_token_secret = os.getenv("X_ACCESS_TOKEN_SECRET")

    # Validate that all credentials are present
    if not all([api_key, api_key_secret, access_token, access_token_secret]):
        print("\nError: Missing API credentials.")
        print("Please make sure you have a .env file with all the required keys:")
        print("X_API_KEY, X_API_KEY_SECRET, X_ACCESS_TOKEN, X_ACCESS_TOKEN_SECRET")
        print("\nYou can use the .env.example file as a template.")
        sys.exit(1)

    print("\nSuccessfully loaded API credentials.")

    # Authenticate with X API
    try:
        print("Authenticating with the X API...")
        client = tweepy.Client(
            consumer_key=api_key,
            consumer_secret=api_key_secret,
            access_token=access_token,
            access_token_secret=access_token_secret,
        )
        # Get authenticated user's ID
        auth_user = client.get_me(user_fields=["id"])
        auth_user_id = auth_user.data.id
        print("Authentication successful.")
    except Exception as e:
        print(f"\nError during API authentication: {e}")
        print("Please check your API credentials and permissions.")
        sys.exit(1)

    # Fetch blocked users
    print("\nFetching blocked accounts... (This might take a moment)")
    blocked_users = []
    try:
        # Tweepy's Paginator handles the pagination for us
        for response in tweepy.Paginator(client.get_blocking, id=auth_user_id, max_results=1000):
            if response.data:
                blocked_users.extend(response.data)

        if not blocked_users:
            print("You have no blocked accounts. Nothing to do!")
            sys.exit(0)

        print(f"Found {len(blocked_users)} blocked accounts.")

    except Exception as e:
        print(f"\nError fetching blocked accounts: {e}")
        sys.exit(1)

    # --- Unblocking Process ---
    print("\nStarting the unblocking process...")
    print("IMPORTANT: The script will pause for 15 minutes after every 50 unblocks to comply with API rate limits.")
    total_blocked = len(blocked_users)
    estimated_minutes = (total_blocked // 50) * 15
    print(f"Based on {total_blocked} accounts, the estimated time to complete is around {estimated_minutes} minutes.")

    unblocked_count = 0
    unblocked_usernames = []
    requests_count = 0

    for user in blocked_users:
        try:
            # Unblock the user
            client.unblock(target_user_id=user.id)

            # Log success
            unblocked_count += 1
            unblocked_usernames.append(user.username)
            print(f"({unblocked_count}/{total_blocked}) Successfully unblocked @{user.username}")

            # Handle rate limiting
            requests_count += 1
            if requests_count == 50:
                print("\n--- Rate limit reached. Pausing for 15 minutes. ---")
                time.sleep(901) # Pause for 15 minutes and 1 second to be safe
                print("--- Resuming... ---\n")
                requests_count = 0 # Reset counter

        except Exception as e:
            print(f"Could not unblock @{user.username}. Reason: {e}")

    print("\n--- Unblocking Process Complete! ---")
    print(f"Total accounts unblocked: {unblocked_count}")
    if unblocked_usernames:
        print("Unblocked users:", ", ".join(unblocked_usernames))


if __name__ == "__main__":
    main()
