pub fn total() -> i32 {
    arithmetic::add(1, 2)
}

#[cfg(test)]
mod tests {
    use std::io::Write;

    #[test]
    fn sums_correctly() {
        let root = std::env::var("LEVENSHTEIN_SOURCE").expect("run through Levenshtein");
        let mut trace = std::fs::OpenOptions::new()
            .create(true)
            .append(true)
            .open(format!("{root}/test-executions.txt"))
            .unwrap();
        writeln!(trace, "executed").unwrap();
        assert_eq!(super::total(), 3);
    }

    #[cfg(feature = "extra")]
    #[test]
    fn additional_scope() {
        assert_eq!(super::total() * 2, 6);
    }
}
