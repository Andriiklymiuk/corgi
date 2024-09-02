"use strict";(self.webpackChunkcorgi_docs=self.webpackChunkcorgi_docs||[]).push([[9671],{9613:(e,t,a)=>{a.d(t,{Zo:()=>c,kt:()=>d});var r=a(9496);function n(e,t,a){return t in e?Object.defineProperty(e,t,{value:a,enumerable:!0,configurable:!0,writable:!0}):e[t]=a,e}function i(e,t){var a=Object.keys(e);if(Object.getOwnPropertySymbols){var r=Object.getOwnPropertySymbols(e);t&&(r=r.filter((function(t){return Object.getOwnPropertyDescriptor(e,t).enumerable}))),a.push.apply(a,r)}return a}function o(e){for(var t=1;t<arguments.length;t++){var a=null!=arguments[t]?arguments[t]:{};t%2?i(Object(a),!0).forEach((function(t){n(e,t,a[t])})):Object.getOwnPropertyDescriptors?Object.defineProperties(e,Object.getOwnPropertyDescriptors(a)):i(Object(a)).forEach((function(t){Object.defineProperty(e,t,Object.getOwnPropertyDescriptor(a,t))}))}return e}function l(e,t){if(null==e)return{};var a,r,n=function(e,t){if(null==e)return{};var a,r,n={},i=Object.keys(e);for(r=0;r<i.length;r++)a=i[r],t.indexOf(a)>=0||(n[a]=e[a]);return n}(e,t);if(Object.getOwnPropertySymbols){var i=Object.getOwnPropertySymbols(e);for(r=0;r<i.length;r++)a=i[r],t.indexOf(a)>=0||Object.prototype.propertyIsEnumerable.call(e,a)&&(n[a]=e[a])}return n}var s=r.createContext({}),p=function(e){var t=r.useContext(s),a=t;return e&&(a="function"==typeof e?e(t):o(o({},t),e)),a},c=function(e){var t=p(e.components);return r.createElement(s.Provider,{value:t},e.children)},m="mdxType",u={inlineCode:"code",wrapper:function(e){var t=e.children;return r.createElement(r.Fragment,{},t)}},h=r.forwardRef((function(e,t){var a=e.components,n=e.mdxType,i=e.originalType,s=e.parentName,c=l(e,["components","mdxType","originalType","parentName"]),m=p(a),h=n,d=m["".concat(s,".").concat(h)]||m[h]||u[h]||i;return a?r.createElement(d,o(o({ref:t},c),{},{components:a})):r.createElement(d,o({ref:t},c))}));function d(e,t){var a=arguments,n=t&&t.mdxType;if("string"==typeof e||n){var i=a.length,o=new Array(i);o[0]=h;var l={};for(var s in t)hasOwnProperty.call(t,s)&&(l[s]=t[s]);l.originalType=e,l[m]="string"==typeof e?e:n,o[1]=l;for(var p=2;p<i;p++)o[p]=a[p];return r.createElement.apply(null,o)}return r.createElement.apply(null,a)}h.displayName="MDXCreateElement"},7443:(e,t,a)=>{a.r(t),a.d(t,{assets:()=>s,contentTitle:()=>o,default:()=>u,frontMatter:()=>i,metadata:()=>l,toc:()=>p});var r=a(8957),n=(a(9496),a(9613));const i={sidebar_position:1},o="Getting started",l={unversionedId:"intro",id:"intro",title:"Getting started",description:"Let's discover Corgi in less than 10 minutes.",source:"@site/docs/intro.md",sourceDirName:".",slug:"/intro",permalink:"/corgi/docs/intro",draft:!1,tags:[],version:"current",sidebarPosition:1,frontMatter:{sidebar_position:1},sidebar:"tutorialSidebar",next:{title:"What is my purpose?",permalink:"/corgi/docs/why_it_exists"}},s={},p=[{value:"Quick install with Homebrew",id:"quick-install-with-homebrew",level:3},{value:"Vscode extension",id:"vscode-extension",level:3},{value:"Services creation",id:"services-creation",level:2}],c={toc:p},m="wrapper";function u(e){let{components:t,...i}=e;return(0,n.kt)(m,(0,r.Z)({},c,i,{components:t,mdxType:"MDXLayout"}),(0,n.kt)("h1",{id:"getting-started"},"Getting started"),(0,n.kt)("p",null,"Let's discover ",(0,n.kt)("strong",{parentName:"p"},"Corgi in less than 10 minutes"),"."),(0,n.kt)("p",null,(0,n.kt)("img",{alt:"Corgi logo",src:a(3532).Z,width:"1400",height:"1400"})),(0,n.kt)("p",null,"Send someone your project yml file, init and run it in minutes."),(0,n.kt)("p",null,"No more long meetings, explanations of how to run new project with multiple\nmicroservices and configs. Just send corgi-compose.yml file to your team and\ncorgi will do the rest."),(0,n.kt)("p",null,"Auto git cloning, db seeding, concurrent running and much more."),(0,n.kt)("p",null,"While in services you can create whatever you want, but in db services ",(0,n.kt)("strong",{parentName:"p"},"for now it supports"),":"),(0,n.kt)("ul",null,(0,n.kt)("li",{parentName:"ul"},(0,n.kt)("a",{parentName:"li",href:"https://www.postgresql.org"},"postgres"),", ",(0,n.kt)("a",{parentName:"li",href:"https://github.com/Andriiklymiuk/corgi_examples/tree/main/postgres"},"example")),(0,n.kt)("li",{parentName:"ul"},(0,n.kt)("a",{parentName:"li",href:"https://www.mongodb.com"},"mongodb"),", ",(0,n.kt)("a",{parentName:"li",href:"https://github.com/Andriiklymiuk/corgi_examples/blob/main/mongodb/mongodb-go.corgi-compose.yml"},"example")),(0,n.kt)("li",{parentName:"ul"},(0,n.kt)("a",{parentName:"li",href:"https://www.rabbitmq.com"},"rabbitmq"),", ",(0,n.kt)("a",{parentName:"li",href:"https://github.com/Andriiklymiuk/corgi_examples/blob/main/rabbitmq/rabbitmq-go-nestjs.corgi-compose.yml"},"example")),(0,n.kt)("li",{parentName:"ul"},(0,n.kt)("a",{parentName:"li",href:"https://docs.localstack.cloud/user-guide/aws/sqs/"},"aws sqs"),", ",(0,n.kt)("a",{parentName:"li",href:"https://github.com/Andriiklymiuk/corgi_examples/blob/main/aws_sqs/aws_sqs_postgres_go_deno.corgi-compose.yml"},"example")),(0,n.kt)("li",{parentName:"ul"},(0,n.kt)("a",{parentName:"li",href:"https://redis.io"},"redis"),", ",(0,n.kt)("a",{parentName:"li",href:"https://github.com/Andriiklymiuk/corgi_examples/blob/main/redis/redis-bun-expo.corgi-compose.yml"},"example")),(0,n.kt)("li",{parentName:"ul"},(0,n.kt)("a",{parentName:"li",href:"https://redis.io"},"redis-server")),(0,n.kt)("li",{parentName:"ul"},(0,n.kt)("a",{parentName:"li",href:"https://www.mysql.com"},"mysql")),(0,n.kt)("li",{parentName:"ul"},(0,n.kt)("a",{parentName:"li",href:"https://mariadb.org"},"mariadb")),(0,n.kt)("li",{parentName:"ul"},(0,n.kt)("a",{parentName:"li",href:"https://aws.amazon.com/dynamodb/"},"dynamodb")),(0,n.kt)("li",{parentName:"ul"},(0,n.kt)("a",{parentName:"li",href:"https://kafka.apache.org"},"kafka")),(0,n.kt)("li",{parentName:"ul"},(0,n.kt)("a",{parentName:"li",href:"https://www.microsoft.com/en-us/sql-server/sql-server-downloads"},"mssql")),(0,n.kt)("li",{parentName:"ul"},(0,n.kt)("a",{parentName:"li",href:"https://cassandra.apache.org/_/index.html"},"cassandra")),(0,n.kt)("li",{parentName:"ul"},(0,n.kt)("a",{parentName:"li",href:"https://www.cockroachlabs.com"},"cockroachDb")),(0,n.kt)("li",{parentName:"ul"},(0,n.kt)("a",{parentName:"li",href:"https://clickhouse.com"},"clickhouse")),(0,n.kt)("li",{parentName:"ul"},(0,n.kt)("a",{parentName:"li",href:"https://www.scylladb.com"},"scylla")),(0,n.kt)("li",{parentName:"ul"},(0,n.kt)("a",{parentName:"li",href:"https://docs.keydb.dev"},"keydb")),(0,n.kt)("li",{parentName:"ul"},(0,n.kt)("a",{parentName:"li",href:"https://www.influxdata.com"},"influxdb")),(0,n.kt)("li",{parentName:"ul"},(0,n.kt)("a",{parentName:"li",href:"https://surrealdb.com"},"surrealdb")),(0,n.kt)("li",{parentName:"ul"},(0,n.kt)("a",{parentName:"li",href:"https://neo4j.com"},"neo4j")),(0,n.kt)("li",{parentName:"ul"},(0,n.kt)("a",{parentName:"li",href:"https://arangodb.com"},"arangodb")),(0,n.kt)("li",{parentName:"ul"},(0,n.kt)("a",{parentName:"li",href:"https://www.elastic.co/elasticsearch#"},"elasticsearch")),(0,n.kt)("li",{parentName:"ul"},(0,n.kt)("a",{parentName:"li",href:"https://www.timescale.com"},"timescaledb")),(0,n.kt)("li",{parentName:"ul"},(0,n.kt)("a",{parentName:"li",href:"https://couchdb.apache.org"},"couchdb")),(0,n.kt)("li",{parentName:"ul"},(0,n.kt)("a",{parentName:"li",href:"https://dgraph.io"},"dgraph")),(0,n.kt)("li",{parentName:"ul"},(0,n.kt)("a",{parentName:"li",href:"https://www.meilisearch.com"},"meilisearch")),(0,n.kt)("li",{parentName:"ul"},(0,n.kt)("a",{parentName:"li",href:"https://fauna.com"},"faunadb")),(0,n.kt)("li",{parentName:"ul"},(0,n.kt)("a",{parentName:"li",href:"https://www.yugabyte.com"},"yugabytedb")),(0,n.kt)("li",{parentName:"ul"},(0,n.kt)("a",{parentName:"li",href:"https://skytable.io"},"skytable")),(0,n.kt)("li",{parentName:"ul"},(0,n.kt)("a",{parentName:"li",href:"https://www.dragonflydb.io"},"dragonfly")),(0,n.kt)("li",{parentName:"ul"},(0,n.kt)("a",{parentName:"li",href:"https://redict.io"},"redict")),(0,n.kt)("li",{parentName:"ul"},(0,n.kt)("a",{parentName:"li",href:"https://github.com/valkey-io/valkey"},"valkey")),(0,n.kt)("li",{parentName:"ul"},(0,n.kt)("a",{parentName:"li",href:"https://docs.localstack.cloud/user-guide/aws/s3/"},"s3"))),(0,n.kt)("h3",{id:"quick-install-with-homebrew"},"Quick install with ",(0,n.kt)("a",{parentName:"h3",href:"https://brew.sh"},"Homebrew")),(0,n.kt)("pre",null,(0,n.kt)("code",{parentName:"pre",className:"language-bash"},"brew install andriiklymiuk/homebrew-tools/corgi\n\n# ask for help to check if it works\ncorgi -h\n")),(0,n.kt)("p",null,"It will install it globally."),(0,n.kt)("p",null,"With it you can run ",(0,n.kt)("inlineCode",{parentName:"p"},"corgi")," in any folder on your local."),(0,n.kt)("p",null,(0,n.kt)("a",{parentName:"p",href:"#services-creation"},"Create service file"),", if you want to run corgi."),(0,n.kt)("p",null,"Try it with expo + hono server example"),(0,n.kt)("pre",null,(0,n.kt)("code",{parentName:"pre",className:"language-bash"},"corgi run -t https://github.com/Andriiklymiuk/corgi_examples/blob/main/honoExpoTodo/hono-bun-expo.corgi-compose.yml\n")),(0,n.kt)("h3",{id:"vscode-extension"},"Vscode extension"),(0,n.kt)("p",null,"We also recommend installing\n",(0,n.kt)("a",{parentName:"p",href:"https://marketplace.visualstudio.com/items?itemName=Corgi.corgi"},"corgi vscode extension"),"\nwhich has syntax helpers, autocompletion and commonly used commands. You can\ncheck and run corgi showcase examples from extension too."),(0,n.kt)("h2",{id:"services-creation"},"Services creation"),(0,n.kt)("p",null,"Corgi has several concepts to understand:"),(0,n.kt)("ul",null,(0,n.kt)("li",{parentName:"ul"},"db_services - database configs to use when doing creation/seeding/etc"),(0,n.kt)("li",{parentName:"ul"},"services - project folders to use for corgi. Can be server, app, anything you\ncan imagine"),(0,n.kt)("li",{parentName:"ul"},"required - programs needed for running your project successfully\n(node,yarn,go,whatever you want). They are checked on init")),(0,n.kt)("p",null,"These items are added to corgi-compose.yml file to create services, db services\nand check for required software."),(0,n.kt)("p",null,"Examples of corgi-compose.yml files are in\n",(0,n.kt)("a",{parentName:"p",href:"https://github.com/Andriiklymiuk/corgi_examples"},"examples repo"),". You can also\ncheck what should be in corgi-compose.yml by running ",(0,n.kt)("inlineCode",{parentName:"p"},"corgi docs"),". It will print\nout all possible items in corgi .yml file or you can go to\n",(0,n.kt)("a",{parentName:"p",href:"corgi_compose_items"},"corgi compose items doc")," to see what the syntax and\npossible values of corgi-compose.yml"),(0,n.kt)("p",null,"After creating corgi-compose.yml file, you can run to create db folders, clone\ngit repos, etc."),(0,n.kt)("pre",null,(0,n.kt)("code",{parentName:"pre",className:"language-bash"},"corgi init\n")),(0,n.kt)("p",null,"If you want to just run services and already created db_services:"),(0,n.kt)("pre",null,(0,n.kt)("code",{parentName:"pre",className:"language-bash"},"corgi run\n")),(0,n.kt)("p",null,(0,n.kt)("em",{parentName:"p"},(0,n.kt)("strong",{parentName:"em"},"Tip")),": there can be as many services as you wish. But create it with\ndifferent ports to be able to run in all at the same time, if you want."),(0,n.kt)("p",null,"You can read of what exactly happens on\n",(0,n.kt)("a",{parentName:"p",href:"why_it_exists#what-happens-on-init"},"run")," or on\n",(0,n.kt)("a",{parentName:"p",href:"why_it_exists#what-happens-on-init"},"init")," to better understand corgi logic."))}u.isMDXComponent=!0},3532:(e,t,a)=>{a.d(t,{Z:()=>r});const r=a.p+"assets/images/corgi-721499a794506d2cb81bfa5cca7848e5.png"}}]);