"""Add a Binder to decode request parameters into typed structs (fix for issue #42)"""

from dataclasses import dataclass
from urllib.parse import parse_qs

@dataclass
class DecodeRequest:
    required: list[str]
    optional: dict[str, type]
    
    def from_query_string(self, query_string: str) -> dict:
        params = parse_qs(query_string)
        decoded = {}
        
        # Validate required parameters
        for key in self.required:
            if key not in params:
                raise ValueError(f"Missing required parameter: {key}")
            value = params[key][0]  # Take first value if multiple
            
            try:
                decoded[key] = self.optional.get(key, str)(value)
            except ValueError:
                raise ValueError(f"Invalid type for {key}: {value}")
        
        # Process optional parameters with type conversion
        for key, type_ in self.optional.items():
            if key in params:
                value_str = params[key][0]
                try:
                    decoded[key] = type_(value_str)
                except ValueError:
                    raise ValueError(f"Invalid value '{value_str}' for parameter {key}")
        
        return decoded

# Example usage:
if __name__ == "__main__":
    request_spec = DecodeRequest(
        required=["id", "timestamp"],
        optional={
            "verbose": bool,
            "filter": str,
            "limit": int
        }
    )
    
    # Sample query string with optional parameters
    query = "id=123&timestamp=2023-01-01T00:00:00Z&verbose=true&filter=active&limit=10"
    
    try:
        decoded = request_spec.from_query_string(query)
        print(decoded)
    except ValueError as e:
        print(f"Error processing request: {str(e)}")

