#include <string>
class Greeter {
 public:
  std::string greet();
};
std::string Greeter::greet() { std::string s("hi"); return s; }
