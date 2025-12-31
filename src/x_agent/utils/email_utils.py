import logging
import aiosmtplib
from email.message import EmailMessage
from ..config import settings


async def send_report_email(report_text: str):
    """
    Sends the generated insights report via email.
    """
    logging.info(f"Preparing to send report email to {settings.report_recipient}...")

    try:
        settings.check_email_config()
    except ValueError as e:
        logging.error(f"Email configuration error: {e}")
        return

    message = EmailMessage()
    message["From"] = settings.smtp_user
    message["To"] = settings.report_recipient
    message["Subject"] = f"X Account Insights Report - {settings.environment.upper()}"

    # We use a code block for the report to preserve formatting in email clients
    html_content = textwrap.dedent(f"""
    <html>
    <body style="font-family: monospace; white-space: pre; background-color: #f4f4f4; padding: 20px;">
        <div style="background-color: white; padding: 20px; border-radius: 8px; box-shadow: 0 2px 4px rgba(0,0,0,0.1);">
            {html.escape(report_text)}
        </div>
        <p style="font-family: sans-serif; color: #666; font-size: 12px; margin-top: 20px;">
            Sent by X-Agent Framework.
        </p>
    </body>
    </html>
    """)
    message.set_content(report_text)
    message.add_alternative(html_content, subtype="html")

    try:
        await aiosmtplib.send(
            message,
            hostname=settings.smtp_host,
            port=settings.smtp_port,
            username=settings.smtp_user,
            password=settings.smtp_password,
            use_tls=settings.smtp_use_tls,
            start_tls=settings.smtp_start_tls,
        )
        logging.info("Report email sent successfully!")
    except aiosmtplib.SMTPException as e:
        logging.error(f"SMTP error occurred: {e}")
    except Exception as e:
        logging.error(f"Unexpected error sending email: {e}", exc_info=True)
