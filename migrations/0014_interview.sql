-- +goose Up
-- Interview practice: multiple-choice questions by domain and difficulty,
-- and every answer a user gives (for their stats).
CREATE TABLE interview_questions (
  id          BIGSERIAL PRIMARY KEY,
  domain      TEXT NOT NULL,
  kind        TEXT NOT NULL CHECK (kind IN ('technical','behavioral')),
  difficulty  TEXT NOT NULL CHECK (difficulty IN ('easy','medium','hard')),
  question    TEXT NOT NULL,
  choices     TEXT[] NOT NULL CHECK (cardinality(choices) = 4),
  answer      SMALLINT NOT NULL CHECK (answer BETWEEN 0 AND 3),
  explanation TEXT NOT NULL,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX interview_questions_pick_idx ON interview_questions (domain, difficulty);

CREATE TABLE interview_attempts (
  id          BIGSERIAL PRIMARY KEY,
  user_id     BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  question_id BIGINT NOT NULL REFERENCES interview_questions(id) ON DELETE CASCADE,
  choice      SMALLINT NOT NULL,
  correct     BOOLEAN NOT NULL,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX interview_attempts_user_idx ON interview_attempts (user_id, created_at DESC);

-- A starter set; more questions are added later (wera interview import).
INSERT INTO interview_questions (domain, kind, difficulty, question, choices, answer, explanation) VALUES
('Data structures & algorithms','technical','easy','What is the average time complexity of looking up a key in a hash table?',
 ARRAY['O(1)','O(log n)','O(n)','O(n log n)'],0,'Hashing maps a key straight to a bucket, so a lookup is constant time on average; it degrades to O(n) only when many keys collide.'),
('Data structures & algorithms','technical','easy','Which data structure gives first-in, first-out order?',
 ARRAY['Stack','Queue','Binary heap','Trie'],1,'A queue removes elements in the order they were added (FIFO); a stack is last-in, first-out.'),
('Data structures & algorithms','technical','medium','You need the k largest numbers from a stream of n numbers using little memory. What is the best approach?',
 ARRAY['Sort everything, take the last k','A min-heap of size k','A max-heap of all n items','A balanced BST of all n items'],1,'Keep a min-heap of the k largest seen so far: replace its minimum when a bigger number arrives. O(n log k) time, O(k) memory.'),
('Data structures & algorithms','technical','medium','Which algorithm finds shortest paths from one source in a graph with non-negative edge weights?',
 ARRAY['Depth-first search','Dijkstra''s algorithm','Kruskal''s algorithm','Topological sort'],1,'Dijkstra greedily settles the closest unsettled vertex; it needs non-negative weights (Bellman-Ford handles negative ones).'),
('Data structures & algorithms','technical','hard','What is the time complexity of building a binary heap from n unsorted elements with the bottom-up method?',
 ARRAY['O(n)','O(n log n)','O(log n)','O(n^2)'],0,'Sifting down from the last parent costs little for the many low nodes; the sum telescopes to O(n), better than n inserts at O(n log n).'),
('Data structures & algorithms','technical','hard','A dynamic programming solution recomputes the same subproblems. What fixes it with the least code change?',
 ARRAY['Memoization','Greedy choice','Divide and conquer','Backtracking'],0,'Caching subproblem results (memoization) turns the exponential recursion into polynomial time without restructuring the code.'),
('System design','technical','easy','What does a load balancer primarily do?',
 ARRAY['Encrypts traffic between services','Spreads requests across several servers','Stores session data','Compresses responses'],1,'It distributes incoming requests across a pool of servers for throughput and availability.'),
('System design','technical','medium','A read-heavy service hits its database too hard. What is usually the first fix?',
 ARRAY['Shard the database','Add a cache in front of it','Switch to a NoSQL store','Add more write replicas'],1,'Caching hot reads (e.g. Redis) removes most read load cheaply; sharding is far more complex and helps writes and size more than reads.'),
('System design','technical','medium','In the CAP theorem, during a network partition a system must choose between:',
 ARRAY['Consistency and availability','Latency and throughput','Durability and speed','Scalability and cost'],0,'When a partition happens you either refuse some requests (stay consistent) or answer with possibly stale data (stay available).'),
('System design','technical','hard','Why use idempotency keys on a payment API?',
 ARRAY['To encrypt card numbers','So retried requests do not charge twice','To rate-limit clients','To shard payments by user'],1,'Clients retry on timeouts; an idempotency key lets the server recognize the retry and return the first result instead of charging again.'),
('Databases','technical','easy','What does an index on a column mainly speed up?',
 ARRAY['Inserts','Lookups and range queries on that column','Backups','Schema changes'],1,'An index (usually a B-tree) finds matching rows without scanning the table; it slightly slows writes.'),
('Databases','technical','medium','Which isolation level prevents dirty reads but still allows non-repeatable reads?',
 ARRAY['Read uncommitted','Read committed','Repeatable read','Serializable'],1,'Read committed only shows committed data, but a row can change between two reads in the same transaction.'),
('Databases','technical','hard','A query filters on (country, signup_date). Which composite index serves "WHERE country = ? AND signup_date > ?" best?',
 ARRAY['(signup_date, country)','(country, signup_date)','Two separate single-column indexes','A hash index on country'],1,'Put the equality column first and the range column last, so the index narrows to one country and then scans a date range.'),
('Operating systems & networking','technical','easy','Which protocol guarantees ordered, reliable delivery?',
 ARRAY['UDP','TCP','ICMP','ARP'],1,'TCP adds sequencing, acknowledgments and retransmission on top of IP; UDP sends datagrams with no such guarantees.'),
('Operating systems & networking','technical','medium','What is the main difference between a process and a thread?',
 ARRAY['Threads cannot run in parallel','Threads share their process''s memory; processes have separate address spaces','Processes are always faster','Threads need their own file descriptors'],1,'Threads of one process share memory and resources, which makes communication cheap but needs synchronization.'),
('Operating systems & networking','technical','hard','What happens first when you type a URL and press enter?',
 ARRAY['A TLS handshake','A DNS lookup of the host name','An HTTP GET','A TCP FIN'],1,'The browser must resolve the host to an IP address (DNS, often cached) before it can open a TCP connection and negotiate TLS.'),
('Machine learning','technical','easy','Your model does great on training data and poorly on new data. This is:',
 ARRAY['Underfitting','Overfitting','Data leakage','Class imbalance'],1,'Overfitting means the model memorized the training set; regularization, more data or a simpler model help.'),
('Machine learning','technical','medium','For a fraud dataset where 0.5% of rows are fraud, which metric is most useful?',
 ARRAY['Accuracy','Precision-recall (e.g. PR AUC)','Mean squared error','R squared'],1,'With heavy imbalance accuracy is misleading (always "not fraud" scores 99.5%); precision and recall show how well the rare class is found.'),
('Machine learning','technical','hard','Why can a feature computed with information from after the prediction time ruin a model?',
 ARRAY['It slows training','It causes data leakage and over-optimistic validation','It needs normalization','It increases variance only'],1,'Future information is not available at prediction time, so validation looks great and production performance collapses.'),
('Behavioral','behavioral','easy','In a STAR answer, what does the R stand for?',
 ARRAY['Reflection','Result','Role','Risk'],1,'Situation, Task, Action, Result: close with the measurable outcome of what you did.'),
('Behavioral','behavioral','medium','Asked about a time you disagreed with a teammate, which answer is strongest?',
 ARRAY['You were right and they eventually admitted it','You avoided the conflict to keep the peace','You understood their view, aligned on data, and committed to the decision','You escalated to your manager immediately'],2,'Interviewers look for listening, using evidence, and disagree-and-commit, not winning or avoiding the conflict.'),
('Behavioral','behavioral','medium','When asked "What is your biggest weakness?", what works best?',
 ARRAY['Claim you are a perfectionist','Name a real, non-critical weakness and what you are doing about it','Say you have none','Describe a weakness that is core to the role'],1,'A genuine weakness plus concrete steps to improve shows self-awareness without raising a red flag for the job.'),
('Behavioral','behavioral','hard','You missed a deadline that affected another team. In the interview story, what matters most?',
 ARRAY['Explaining whose fault it really was','Owning it, how you communicated early, the fix, and what you changed afterwards','Showing the deadline was unrealistic','Keeping the story short to move on'],1,'Ownership, proactive communication and a durable process change are what interviewers listen for in failure stories.');

-- +goose Down
DROP TABLE interview_attempts;
DROP TABLE interview_questions;
