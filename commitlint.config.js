/**
 * Commitlint configuration for HyperFleet Sentinel
 *
 * Enforces HyperFleet Commit Message Standard:
 * https://github.com/openshift-hyperfleet/architecture/blob/main/hyperfleet/standards/commit-standard.md
 *
 * Valid formats:
 * - HYPERFLEET-XXX - type: subject
 * - type: subject (when no JIRA ticket)
 */

module.exports = {
  extends: ['@commitlint/config-conventional'],

  rules: {
    // Header format allows optional HYPERFLEET-XXX prefix
    'header-max-length': [2, 'always', 100],

    // Subject line (excluding ticket prefix) must not exceed 72 characters
    'subject-max-length': [2, 'always', 72],

    // Enforce lowercase for type
    'type-case': [2, 'always', 'lower-case'],

    // Allowed types per HyperFleet standard (includes extended types: style, perf)
    'type-enum': [
      2,
      'always',
      [
        'feat',      // New feature
        'fix',       // Bug fix
        'docs',      // Documentation changes
        'style',     // Code style (HyperFleet extension: gofmt, goimports)
        'refactor',  // Code restructuring
        'perf',      // Performance improvements (HyperFleet extension)
        'test',      // Adding tests
        'build',     // Build system changes
        'ci',        // CI configuration
        'chore',     // Other changes
        'revert'     // Reverting commits
      ]
    ],

    // Subject must not be empty
    'subject-empty': [2, 'never'],

    // Subject must not end with period
    'subject-full-stop': [2, 'never', '.'],

    // Subject should start with lowercase (after type:)
    'subject-case': [2, 'always', 'lower-case'],

    // Type must not be empty
    'type-empty': [2, 'never'],

    // Body must have leading blank line if present
    'body-leading-blank': [2, 'always'],

    // Footer must have leading blank line if present
    'footer-leading-blank': [2, 'always'],

    // Scope is optional
    'scope-empty': [0]
  },

  // Custom parser to handle HYPERFLEET-XXX prefix
  parserPreset: {
    parserOpts: {
      // Matches:
      // - HYPERFLEET-123 - feat: description
      // - feat: description
      // - feat(scope): description
      headerPattern: /^(?:HYPERFLEET-\d+\s+-\s+)?(\w+)(?:\(([^)]*)\))?:\s+(.+)$/,
      headerCorrespondence: ['type', 'scope', 'subject']
    }
  },

  // Ignore merge commits and revert commits (automatically generated)
  ignores: [
    (commit) => commit.startsWith('Merge branch'),
    (commit) => commit.startsWith('Merge pull request'),
    (commit) => commit.startsWith('Revert "')
  ],

  // Show help URL in error messages
  helpUrl: 'https://github.com/openshift-hyperfleet/architecture/blob/main/hyperfleet/standards/commit-standard.md'
};
