import os
from os import path
class Greeter:
    def greet(self):
        return path.join(os.getcwd(), "hi")
