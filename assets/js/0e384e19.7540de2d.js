"use strict";(self.webpackChunkcorgi_docs=self.webpackChunkcorgi_docs||[]).push([[9671],{9613:(e,t,n)=>{n.d(t,{Zo:()=>p,kt:()=>g});var r=n(9496);function a(e,t,n){return t in e?Object.defineProperty(e,t,{value:n,enumerable:!0,configurable:!0,writable:!0}):e[t]=n,e}function i(e,t){var n=Object.keys(e);if(Object.getOwnPropertySymbols){var r=Object.getOwnPropertySymbols(e);t&&(r=r.filter((function(t){return Object.getOwnPropertyDescriptor(e,t).enumerable}))),n.push.apply(n,r)}return n}function o(e){for(var t=1;t<arguments.length;t++){var n=null!=arguments[t]?arguments[t]:{};t%2?i(Object(n),!0).forEach((function(t){a(e,t,n[t])})):Object.getOwnPropertyDescriptors?Object.defineProperties(e,Object.getOwnPropertyDescriptors(n)):i(Object(n)).forEach((function(t){Object.defineProperty(e,t,Object.getOwnPropertyDescriptor(n,t))}))}return e}function l(e,t){if(null==e)return{};var n,r,a=function(e,t){if(null==e)return{};var n,r,a={},i=Object.keys(e);for(r=0;r<i.length;r++)n=i[r],t.indexOf(n)>=0||(a[n]=e[n]);return a}(e,t);if(Object.getOwnPropertySymbols){var i=Object.getOwnPropertySymbols(e);for(r=0;r<i.length;r++)n=i[r],t.indexOf(n)>=0||Object.prototype.propertyIsEnumerable.call(e,n)&&(a[n]=e[n])}return a}var s=r.createContext({}),c=function(e){var t=r.useContext(s),n=t;return e&&(n="function"==typeof e?e(t):o(o({},t),e)),n},p=function(e){var t=c(e.components);return r.createElement(s.Provider,{value:t},e.children)},u="mdxType",m={inlineCode:"code",wrapper:function(e){var t=e.children;return r.createElement(r.Fragment,{},t)}},d=r.forwardRef((function(e,t){var n=e.components,a=e.mdxType,i=e.originalType,s=e.parentName,p=l(e,["components","mdxType","originalType","parentName"]),u=c(n),d=a,g=u["".concat(s,".").concat(d)]||u[d]||m[d]||i;return n?r.createElement(g,o(o({ref:t},p),{},{components:n})):r.createElement(g,o({ref:t},p))}));function g(e,t){var n=arguments,a=t&&t.mdxType;if("string"==typeof e||a){var i=n.length,o=new Array(i);o[0]=d;var l={};for(var s in t)hasOwnProperty.call(t,s)&&(l[s]=t[s]);l.originalType=e,l[u]="string"==typeof e?e:a,o[1]=l;for(var c=2;c<i;c++)o[c]=n[c];return r.createElement.apply(null,o)}return r.createElement.apply(null,n)}d.displayName="MDXCreateElement"},7443:(e,t,n)=>{n.r(t),n.d(t,{assets:()=>s,contentTitle:()=>o,default:()=>m,frontMatter:()=>i,metadata:()=>l,toc:()=>c});var r=n(8957),a=(n(9496),n(9613));const i={sidebar_position:1},o="Getting started",l={unversionedId:"intro",id:"intro",title:"Getting started",description:"Let's discover Corgi in less than 10 minutes.",source:"@site/docs/intro.md",sourceDirName:".",slug:"/intro",permalink:"/corgi/docs/intro",draft:!1,tags:[],version:"current",sidebarPosition:1,frontMatter:{sidebar_position:1},sidebar:"tutorialSidebar",next:{title:"What is my purpose?",permalink:"/corgi/docs/why_it_exists"}},s={},c=[{value:"Quick install with Homebrew",id:"quick-install-with-homebrew",level:3},{value:"Vscode extension",id:"vscode-extension",level:3},{value:"Services creation",id:"services-creation",level:2}],p={toc:c},u="wrapper";function m(e){let{components:t,...i}=e;return(0,a.kt)(u,(0,r.Z)({},p,i,{components:t,mdxType:"MDXLayout"}),(0,a.kt)("h1",{id:"getting-started"},"Getting started"),(0,a.kt)("p",null,"Let's discover ",(0,a.kt)("strong",{parentName:"p"},"Corgi in less than 10 minutes"),"."),(0,a.kt)("p",null,(0,a.kt)("img",{alt:"Corgi logo",src:n(3532).Z,width:"1400",height:"1400"})),(0,a.kt)("p",null,"Send someone your project yml file, init and run it in minutes."),(0,a.kt)("p",null,"No more long meetings, explanations of how to run new project with multiple\nmicroservices and configs. Just send corgi-compose.yml file to your team and\ncorgi will do the rest."),(0,a.kt)("p",null,"Auto git cloning, db seeding, concurrent running and much more."),(0,a.kt)("p",null,"While in services you can create whatever you want, but in db services ",(0,a.kt)("strong",{parentName:"p"},"for now\nit supports"),":"),(0,a.kt)("ul",null,(0,a.kt)("li",{parentName:"ul"},(0,a.kt)("a",{parentName:"li",href:"https://github.com/Andriiklymiuk/corgi_examples/tree/main/postgres"},"postgres")),(0,a.kt)("li",{parentName:"ul"},(0,a.kt)("a",{parentName:"li",href:"https://github.com/Andriiklymiuk/corgi_examples/blob/main/mongodb/mongodb-go.corgi-compose.yml"},"mongodb")),(0,a.kt)("li",{parentName:"ul"},(0,a.kt)("a",{parentName:"li",href:"https://github.com/Andriiklymiuk/corgi_examples/blob/main/rabbitmq/rabbitmq-go-nestjs.corgi-compose.yml"},"rabbitmq")),(0,a.kt)("li",{parentName:"ul"},(0,a.kt)("a",{parentName:"li",href:"https://github.com/Andriiklymiuk/corgi_examples/blob/main/aws_sqs/aws_sqs_postgres_go_deno.corgi-compose.yml"},"aws sqs")),(0,a.kt)("li",{parentName:"ul"},(0,a.kt)("a",{parentName:"li",href:"https://github.com/Andriiklymiuk/corgi_examples/blob/main/redis/redis-bun-expo.corgi-compose.yml"},"redis")),(0,a.kt)("li",{parentName:"ul"},"mysql"),(0,a.kt)("li",{parentName:"ul"},"mariadb"),(0,a.kt)("li",{parentName:"ul"},"dynamodb"),(0,a.kt)("li",{parentName:"ul"},"kafka"),(0,a.kt)("li",{parentName:"ul"},"mssql"),(0,a.kt)("li",{parentName:"ul"},"cassandra"),(0,a.kt)("li",{parentName:"ul"},"cockroach"),(0,a.kt)("li",{parentName:"ul"},"clickhouse"),(0,a.kt)("li",{parentName:"ul"},"scylla"),(0,a.kt)("li",{parentName:"ul"},"keydb"),(0,a.kt)("li",{parentName:"ul"},"influxdb"),(0,a.kt)("li",{parentName:"ul"},"surrealdb"),(0,a.kt)("li",{parentName:"ul"},"arangodb"),(0,a.kt)("li",{parentName:"ul"},"neo4j"),(0,a.kt)("li",{parentName:"ul"},"elasticsearch"),(0,a.kt)("li",{parentName:"ul"},"timescaledb"),(0,a.kt)("li",{parentName:"ul"},"couchdb"),(0,a.kt)("li",{parentName:"ul"},"dgraph"),(0,a.kt)("li",{parentName:"ul"},"meilisearch"),(0,a.kt)("li",{parentName:"ul"},"faunadb")),(0,a.kt)("h3",{id:"quick-install-with-homebrew"},"Quick install with ",(0,a.kt)("a",{parentName:"h3",href:"https://brew.sh"},"Homebrew")),(0,a.kt)("pre",null,(0,a.kt)("code",{parentName:"pre",className:"language-bash"},"brew install andriiklymiuk/homebrew-tools/corgi\n\n# ask for help to check if it works\ncorgi -h\n")),(0,a.kt)("p",null,"It will install it globally."),(0,a.kt)("p",null,"With it you can run ",(0,a.kt)("inlineCode",{parentName:"p"},"corgi")," in any folder on your local."),(0,a.kt)("p",null,(0,a.kt)("a",{parentName:"p",href:"#services-creation"},"Create service file"),", if you want to run corgi."),(0,a.kt)("h3",{id:"vscode-extension"},"Vscode extension"),(0,a.kt)("p",null,"We also recommend installing\n",(0,a.kt)("a",{parentName:"p",href:"https://marketplace.visualstudio.com/items?itemName=Corgi.corgi"},"corgi vscode extension"),"\nwhich has syntax helpers, autocompletion and commonly used commands. You can\ncheck and run corgi showcase examples from extension too."),(0,a.kt)("h2",{id:"services-creation"},"Services creation"),(0,a.kt)("p",null,"Corgi has several concepts to understand:"),(0,a.kt)("ul",null,(0,a.kt)("li",{parentName:"ul"},"db_services - database configs to use when doing creation/seeding/etc"),(0,a.kt)("li",{parentName:"ul"},"services - project folders to use for corgi. Can be server, app, anything you\ncan imagine"),(0,a.kt)("li",{parentName:"ul"},"required - programs needed for running your project successfully\n(node,yarn,go,whatever you want). They are checked on init")),(0,a.kt)("p",null,"These items are added to corgi-compose.yml file to create services, db services\nand check for required software."),(0,a.kt)("p",null,"Examples of corgi-compose.yml files are in\n",(0,a.kt)("a",{parentName:"p",href:"https://github.com/Andriiklymiuk/corgi_examples"},"examples repo"),". You can also\ncheck what should be in corgi-compose.yml by running ",(0,a.kt)("inlineCode",{parentName:"p"},"corgi docs"),". It will print\nout all possible items in corgi .yml file or you can go to\n",(0,a.kt)("a",{parentName:"p",href:"corgi_compose_items"},"corgi compose items doc")," to see what the syntax and\npossible values of corgi-compose.yml"),(0,a.kt)("p",null,"After creating corgi-compose.yml file, you can run to create db folders, clone\ngit repos, etc."),(0,a.kt)("pre",null,(0,a.kt)("code",{parentName:"pre",className:"language-bash"},"corgi init\n")),(0,a.kt)("p",null,"If you want to just run services and already created db_services:"),(0,a.kt)("pre",null,(0,a.kt)("code",{parentName:"pre",className:"language-bash"},"corgi run\n")),(0,a.kt)("p",null,(0,a.kt)("em",{parentName:"p"},(0,a.kt)("strong",{parentName:"em"},"Tip")),": there can be as many services as you wish. But create it with\ndifferent ports to be able to run in all at the same time, if you want."),(0,a.kt)("p",null,"You can read of what exactly happens on\n",(0,a.kt)("a",{parentName:"p",href:"why_it_exists#what-happens-on-init"},"run")," or on\n",(0,a.kt)("a",{parentName:"p",href:"why_it_exists#what-happens-on-init"},"init")," to better understand corgi logic."))}m.isMDXComponent=!0},3532:(e,t,n)=>{n.d(t,{Z:()=>r});const r=n.p+"assets/images/corgi-721499a794506d2cb81bfa5cca7848e5.png"}}]);