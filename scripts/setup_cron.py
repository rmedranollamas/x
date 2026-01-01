import sys
import shutil
import subprocess
from pathlib import Path


def setup_cron():
    project_dir = Path(__file__).parent.parent.absolute()
    uv_path = shutil.which("uv")

    if not uv_path:
        print("Error: 'uv' not found in PATH. Please make sure it is installed.")
        sys.exit(1)

    print("\n--- 🚀 X-Agent Cron Setup ---")
    print(f"Project Directory: {project_dir}")
    print(f"UV Path:           {uv_path}")

    # Check .env
    env_file = project_dir / ".env"
    if not env_file.exists():
        print("Warning: No .env file found in project root.")
    else:
        with open(env_file, "r") as f:
            content = f.read()
            if "REPORT_RECIPIENT" not in content:
                print(
                    "Warning: REPORT_RECIPIENT not found in .env. Email reporting might fail."
                )

    cron_schedule = "0 9 * * *"
    log_file = project_dir / ".state" / "cron.log"

    # Construct the command
    # We use 'X_AGENT_ENV=production' explicitly to ensure it runs on your real data
    cron_command = f"cd {project_dir} && X_AGENT_ENV=production {uv_path} run x-agent insights --email >> {log_file} 2>&1"
    cron_line = f"{cron_schedule} {cron_command}"

    print("\nProposed Crontab Line:")
    print(f"\033[96m{cron_line}\033[0m")

    confirm = input("\nWould you like to install this daily at 9:00 AM? (y/n): ")
    if confirm.lower() == "y":
        try:
            # Get current crontab
            current_cron = ""
            try:
                current_cron = subprocess.check_output(
                    "crontab -l", shell=True, stderr=subprocess.DEVNULL
                ).decode()
            except subprocess.CalledProcessError:
                pass  # Crontab might be empty

            if cron_command in current_cron:
                print("\nJob already exists in crontab. Skipping.")
                return

            # Add new line
            new_cron = current_cron.rstrip() + "\n" + cron_line + "\n"

            # Write back
            process = subprocess.Popen(["crontab", "-"], stdin=subprocess.PIPE)
            process.communicate(input=new_cron.encode())

            print("\n✅ Cronjob installed successfully!")
            print(f"Logs will be available at: {log_file}")
        except Exception as e:
            print(f"\nFailed to update crontab: {e}")
    else:
        print("\nSetup cancelled.")


if __name__ == "__main__":
    setup_cron()
