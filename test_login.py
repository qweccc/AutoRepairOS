import sys
import os
from playwright.sync_api import sync_playwright

# Path provided by user
CHROMIUM_PATH = r"C:\Users\fr\AppData\Local\hermes\tools\chromium-1248"

def run_test():
    try:
        with sync_playwright() as p:
            # Launching with the specific executable path
            # Note: Playwright usually finds the browser automatically if installed, 
            # but since we have a specific path, we'll try to let it handle the logic.
            # If it fails to find it, we might need to set ENV vars.
            browser = p.chromium.launch(headless=True)
            page = browser.new_page()
            
            print(f"Navigating to http://localhost:8090/login...")
            page.goto('http://localhost:8090/login', wait_until='networkidle')
            
            # Print status and title
            print(f"Status: {page.status}")
            print(f"Title: {page.title()}")
            
            # Wait for the login inputs to appear
            # Using general selectors as we haven't mapped the IDs yet
            print("Waiting for login fields...")
            page.wait_for_selector('input[type="text"], input[type="password"], button')
            
            # Try to find the username input (often type="text" or name="username")
            # We'll look for the first input that isn't a hidden one
            username_input = page.query_selector('input[type="text"]')
            password_input = page.query_selector('input[type="password"]')
            login_button = page.query_selector('button')

            if username_input and password_input and login_button:
                print("Entering credentials...")
                username_input.fill('owner')
                password_input.fill('owner1234!')
                login_button.click()
                
                # Wait for navigation to dashboard
                page.wait_for_timeout(3000)
                print(f"Post-login Title: {page.title()}")
                print(f"Post-login Content Preview: {page.content()[:200]}")
            else:
                print("Could not find login elements. Printing page content for debugging:")
                print(page.content()[:500])

    except Exception as e:
        print(f"An error occurred: {e}")
    finally:
        # Browser closes automatically via context manager
        pass

if __name__ == "__main__":
    run_test()
